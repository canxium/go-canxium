// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethash

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"golang.org/x/crypto/sha3"
)

// Ethash proof-of-work protocol constants.
var (
	FrontierBlockReward       = big.NewInt(5e+18) // Block reward in wei for successfully mining a block
	ByzantiumBlockReward      = big.NewInt(3e+18) // Block reward in wei for successfully mining a block upward from Byzantium
	ConstantinopleBlockReward = big.NewInt(2e+18) // Block reward in wei for successfully mining a block upward from Constantinople

	// PoW 2.0 emission schedule. The per-block reward halves every era and the
	// cumulative emission is hard-capped, so total newly mined supply can never
	// exceed Pow2MiningCap (28.5M CAU). With ~1.5M CAU pre-allocated in genesis
	// this targets a grand total of ~30M CAU.
	//
	// Era length is 5 years at a 3s block time (365.25-day year):
	//   5 * 365.25 * 86400 / 3 = 52,596,000 blocks.
	// The era-0 reward is derived from the cap so the geometric series
	// (R0 + R0/2 + R0/4 + ...) sums to the cap: R0 = cap / (2 * eraBlocks).
	// Era 0 emits half the cap (~14.25M), and the reward integer-shifts to 0
	// after ~58 eras (~290 years). Emission is anchored to wall-clock time, so a
	// change in block time scales the era's block count and the per-block reward
	// inversely, leaving total mined supply and the time-based schedule unchanged.
	Pow2HalvingEraBlocks = uint64(52_596_000)                                        // Blocks per halving era (~5 years at 3s)
	Pow2MiningCap        = new(big.Int).Mul(big.NewInt(28_500_000), big.NewInt(1e18)) // Hard cap on total mined supply, in wei
	Pow2InitialReward    = new(big.Int).Div(Pow2MiningCap, big.NewInt(2*52_596_000))  // Era-0 reward per block, in wei (~0.2709 CAU)

	// Pow2TargetBlockSeconds is the target block interval the difficulty
	// adjustment aims for. See calcDifficultyPoW2.
	Pow2TargetBlockSeconds = uint64(3)

	maxUncles                     = 2        // Maximum number of uncles allowed in a single block
	allowedFutureBlockTimeSeconds = int64(7) // Max seconds from current time allowed for blocks, before they're considered future blocks
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	errOlderBlockTime       = errors.New("timestamp older than parent")
	errTooManyUncles        = errors.New("too many uncles")
	errDuplicateUncle       = errors.New("duplicate uncle")
	errUncleIsAncestor      = errors.New("uncle is ancestor")
	errDanglingUncle        = errors.New("uncle's parent is not ancestor")
	errInvalidDifficulty    = errors.New("non-positive difficulty")
	errInvalidMixDigest     = errors.New("invalid mix digest")
	errInvalidPoW           = errors.New("invalid proof-of-work")
	errDifficultyUnderValue = errors.New("mining transaction difficulty under value")
	errInvalidMiningTxType  = errors.New("invalid mining transaction type")
	errInvalidMiningTxValue = errors.New("invalid mining transaction value")

	ErrInvalidMiningReceiver  = errors.New("invalid mining transaction receiver")
	ErrInvalidMiningSender    = errors.New("invalid mining transaction sender")
	ErrInvalidMiningInput     = errors.New("invalid mining transaction input data")
	ErrInvalidMiningAlgorithm = errors.New("invalid mining transaction algorithm")
)

// Author implements consensus.Engine, returning the header's coinbase as the
// proof-of-work verified author of the block.
func (ethash *Ethash) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
func (ethash *Ethash) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake {
		return nil
	}
	// Short circuit if the header is known, or its parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Sanity checks passed, do a proper verification
	grandparent, greatGrandparent := difficultyAncestors(chain, nil, parent)
	return ethash.verifyHeader(chain, header, parent, grandparent, greatGrandparent, false, seal, time.Now().Unix())
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func (ethash *Ethash) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake || len(headers) == 0 {
		abort, results := make(chan struct{}), make(chan error, len(headers))
		for i := 0; i < len(headers); i++ {
			results <- nil
		}
		return abort, results
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(headers) < workers {
		workers = len(headers)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs  = make(chan int)
		done    = make(chan int, workers)
		errors  = make([]error, len(headers))
		abort   = make(chan struct{})
		unixNow = time.Now().Unix()
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				errors[index] = ethash.verifyHeaderWorker(chain, headers, seals, index, unixNow)
				done <- index
			}
		}()
	}

	errorsOut := make(chan error, len(headers))
	go func() {
		defer close(inputs)
		var (
			in, out = 0, 0
			checked = make([]bool, len(headers))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(headers) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					errorsOut <- errors[out]
					if out == len(headers)-1 {
						return
					}
				}
			case <-abort:
				return
			}
		}
	}()
	return abort, errorsOut
}

func (ethash *Ethash) verifyHeaderWorker(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool, index int, unixNow int64) error {
	var parent *types.Header
	if index == 0 {
		parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	} else if headers[index-1].Hash() == headers[index].ParentHash {
		parent = headers[index-1]
	}
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Resolve the difficulty ancestors from the in-flight batch first: during
	// sync the grandparent/great-grandparent may be earlier in this same batch and
	// not yet committed to the chain.
	grandparent, greatGrandparent := difficultyAncestors(chain, headers, parent)
	return ethash.verifyHeader(chain, headers[index], parent, grandparent, greatGrandparent, false, seals[index], unixNow)
}

// difficultyAncestors resolves the grandparent and great-grandparent of the block
// whose parent is given — the two ancestors CalcDifficultyPoW2 needs beyond the
// parent. Each lookup prefers the in-flight batch (see ancestorHeader) and falls
// back to the committed chain. Either result may be nil near genesis.
func difficultyAncestors(chain consensus.ChainHeaderReader, batch []*types.Header, parent *types.Header) (grandparent, greatGrandparent *types.Header) {
	if parent.Number.Sign() > 0 {
		grandparent = ancestorHeader(chain, batch, parent.ParentHash, parent.Number.Uint64()-1)
	}
	if grandparent != nil && grandparent.Number.Sign() > 0 {
		greatGrandparent = ancestorHeader(chain, batch, grandparent.ParentHash, grandparent.Number.Uint64()-1)
	}
	return grandparent, greatGrandparent
}

// ancestorHeader returns the header for (hash, number), preferring the in-flight
// verification batch (contiguous, ascending by number) over the committed chain.
// This matters during sync: a block's difficulty ancestors can be earlier headers
// in the same batch that have not been written to the chain yet.
func ancestorHeader(chain consensus.ChainHeaderReader, batch []*types.Header, hash common.Hash, number uint64) *types.Header {
	if len(batch) > 0 {
		base := batch[0].Number.Uint64()
		if number >= base && number < base+uint64(len(batch)) {
			if h := batch[number-base]; h.Hash() == hash {
				return h
			}
		}
	}
	return chain.GetHeader(hash, number)
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of the stock Ethereum ethash engine.
func (ethash *Ethash) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake {
		return nil
	}
	// Verify that there are at most 2 uncles included in this block
	if len(block.Uncles()) > maxUncles {
		return errTooManyUncles
	}
	if len(block.Uncles()) == 0 {
		return nil
	}
	// Gather the set of past uncles and ancestors
	uncles, ancestors := mapset.NewSet[common.Hash](), make(map[common.Hash]*types.Header)

	number, parent := block.NumberU64()-1, block.ParentHash()
	for i := 0; i < 7; i++ {
		ancestorHeader := chain.GetHeader(parent, number)
		if ancestorHeader == nil {
			break
		}
		ancestors[parent] = ancestorHeader
		// If the ancestor doesn't have any uncles, we don't have to iterate them
		if ancestorHeader.UncleHash != types.EmptyUncleHash {
			// Need to add those uncles to the banned list too
			ancestor := chain.GetBlock(parent, number)
			if ancestor == nil {
				break
			}
			for _, uncle := range ancestor.Uncles() {
				uncles.Add(uncle.Hash())
			}
		}
		parent, number = ancestorHeader.ParentHash, number-1
	}
	ancestors[block.Hash()] = block.Header()
	uncles.Add(block.Hash())

	// Verify each of the uncles that it's recent, but not an ancestor
	for _, uncle := range block.Uncles() {
		// Make sure every uncle is rewarded only once
		hash := uncle.Hash()
		if uncles.Contains(hash) {
			return errDuplicateUncle
		}
		uncles.Add(hash)

		// Make sure the uncle has a valid ancestry
		if ancestors[hash] != nil {
			return errUncleIsAncestor
		}
		if ancestors[uncle.ParentHash] == nil || uncle.ParentHash == block.ParentHash() {
			return errDanglingUncle
		}
		uncleParent := ancestors[uncle.ParentHash]
		uncleGrand, uncleGreatGrand := difficultyAncestors(chain, nil, uncleParent)
		if err := ethash.verifyHeader(chain, uncle, uncleParent, uncleGrand, uncleGreatGrand, true, true, time.Now().Unix()); err != nil {
			return err
		}
	}
	return nil
}

// verifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
// See YP section 4.3.4. "Block Header Validity"
func (ethash *Ethash) verifyHeader(chain consensus.ChainHeaderReader, header, parent, grandparent, greatGrandparent *types.Header, uncle bool, seal bool, unixNow int64) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	// Verify the header's timestamp
	if !uncle {
		if header.Time > uint64(unixNow+allowedFutureBlockTimeSeconds) {
			return consensus.ErrFutureBlock
		}
	}
	if header.Time <= parent.Time {
		return errOlderBlockTime
	}
	// Verify the block's difficulty. PoW 2.0 difficulty is derived from the
	// parent and its ancestors (CalcDifficultyPoW2); the ancestors are resolved by
	// the caller so that during batched sync verification they can come from the
	// in-flight batch, not only from the committed chain.
	expected := CalcDifficultyPoW2(parent, grandparent, greatGrandparent)

	if expected.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	}
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	// Verify the block's gas usage and (if applicable) verify the base fee.
	if !chain.Config().IsLondon(header.Number) {
		// Verify BaseFee not present before EIP-1559 fork.
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, expected 'nil'", header.BaseFee)
		}
		if err := misc.VerifyGaslimit(parent.GasLimit, header.GasLimit); err != nil {
			return err
		}
	} else if err := misc.VerifyEip1559Header(chain.Config(), parent, header); err != nil {
		// Verify the header's EIP-1559 attributes.
		return err
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	if chain.Config().IsShanghai(header.Time) {
		return fmt.Errorf("ethash does not support shanghai fork")
	}
	if chain.Config().IsCancun(header.Time) {
		return fmt.Errorf("ethash does not support cancun fork")
	}
	// Verify the engine specific seal securing the block
	if seal {
		if err := ethash.verifySeal(chain, header, false); err != nil {
			return err
		}
	}
	// If all checks passed, validate any special fields for hard forks
	if err := misc.VerifyDAOHeaderExtraData(chain.Config(), header); err != nil {
		return err
	}
	return nil
}

// CalcDifficulty is the difficulty adjustment algorithm. In PoW 2.0 the
// difficulty of a block is a function of its parent's ancestors, not of `time`
// (the new block's own timestamp), which is stamped at seal and unknown while
// the block is being built. It walks two ancestors back from parent to feed
// calcDifficultyPoW2.
func (ethash *Ethash) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	var grandparent, greatGrandparent *types.Header
	if parent.Number.Sign() > 0 {
		grandparent = chain.GetHeader(parent.ParentHash, parent.Number.Uint64()-1)
	}
	if grandparent != nil && grandparent.Number.Sign() > 0 {
		greatGrandparent = chain.GetHeader(grandparent.ParentHash, grandparent.Number.Uint64()-1)
	}
	return CalcDifficultyPoW2(parent, grandparent, greatGrandparent)
}

// CalcDifficulty (package form) is retained for tooling (e.g. the evm t8n
// command) that has no chain reader to walk ancestors. Without the ancestors
// the realized block time cannot be measured, so it holds the parent difficulty.
// The live chain always uses the chain-aware method form above.
func CalcDifficulty(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	return CalcDifficultyPoW2(parent, nil, nil)
}

// CalcDifficultyPoW2 computes the PoW 2.0 block difficulty from the parent and
// the parent's ancestors. The adjustment keys off the realized block time of the
// grandparent (grandparent.Time - greatGrandparent.Time) rather than the block's
// own or its parent's time. Rationale:
//
//   - The block's own timestamp is stamped by the winning miner at seal time and
//     is excluded from SealHash, so it is not knowable when difficulty is fixed.
//   - The miner pipeline pre-builds block N+1 while block N is still being mined,
//     so N's (the parent's) real timestamp is also not yet known. The grandparent
//     and great-grandparent are always sealed and identical on every node, so the
//     speculatively pre-built difficulty matches what verifyHeader recomputes.
//
// This is a two-block adjustment lag; at a 3s target the extra ~6s of reaction is
// immaterial to stability. grandparent/greatGrandparent are nil near genesis, in
// which case difficulty is held at the parent's value (floored at MinimumDifficulty).
func CalcDifficultyPoW2(parent, grandparent, greatGrandparent *types.Header) *big.Int {
	diff := new(big.Int).Set(parent.Difficulty)
	if grandparent != nil && greatGrandparent != nil {
		// algorithm:
		// diff = parent_diff + parent_diff/DifficultyBoundDivisor * max(1 - dt/target, -99)
		// where dt is the grandparent's realized block time in seconds. Neutral
		// (x==0) when target <= dt < 2*target; faster raises difficulty, slower lowers it.
		dt := grandparent.Time - greatGrandparent.Time

		x := new(big.Int).SetInt64(1 - int64(dt/Pow2TargetBlockSeconds))
		if x.Cmp(bigMinus99) < 0 {
			x.Set(bigMinus99)
		}
		y := new(big.Int).Div(parent.Difficulty, params.DifficultyBoundDivisor)
		x.Mul(y, x)
		diff.Add(parent.Difficulty, x)
	}
	if diff.Cmp(params.MinimumDifficulty) < 0 {
		diff.Set(params.MinimumDifficulty)
	}
	return diff
}

// Some weird constants to avoid constant memory allocs for them.
var (
	expDiffPeriod = big.NewInt(100000)
	big0          = big.NewInt(0)
	big1          = big.NewInt(1)
	big2          = big.NewInt(2)
	big6          = big.NewInt(6)
	big9          = big.NewInt(9)
	big10         = big.NewInt(10)
	big100        = big.NewInt(100)
	bigMinus99    = big.NewInt(-99)
)


// makeDifficultyCalculator creates a difficultyCalculator with the given bomb-delay.
// the difficulty is calculated with Byzantium rules, which differs from Homestead in
// how uncles affect the calculation
func makeDifficultyCalculator(bombDelay *big.Int) func(time uint64, parent *types.Header) *big.Int {
	// Note, the calculations below looks at the parent number, which is 1 below
	// the block number. Thus we remove one from the delay given
	bombDelayFromParent := new(big.Int).Sub(bombDelay, big1)
	return func(time uint64, parent *types.Header) *big.Int {
		// https://github.com/ethereum/EIPs/issues/100.
		// algorithm:
		// diff = (parent_diff +
		//         (parent_diff / 2048 * max((2 if len(parent.uncles) else 1) - ((timestamp - parent.timestamp) // 9), -99))
		//        ) + 2^(periodCount - 2)

		bigTime := new(big.Int).SetUint64(time)
		bigParentTime := new(big.Int).SetUint64(parent.Time)

		// holds intermediate values to make the algo easier to read & audit
		x := new(big.Int)
		y := new(big.Int)

		// (2 if len(parent_uncles) else 1) - (block_timestamp - parent_timestamp) // 9
		x.Sub(bigTime, bigParentTime)
		x.Div(x, big9)
		if parent.UncleHash == types.EmptyUncleHash {
			x.Sub(big1, x)
		} else {
			x.Sub(big2, x)
		}
		// max((2 if len(parent_uncles) else 1) - (block_timestamp - parent_timestamp) // 9, -99)
		if x.Cmp(bigMinus99) < 0 {
			x.Set(bigMinus99)
		}
		// parent_diff + (parent_diff / 2048 * max((2 if len(parent.uncles) else 1) - ((timestamp - parent.timestamp) // 9), -99))
		y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
		x.Mul(y, x)
		x.Add(parent.Difficulty, x)

		// minimum difficulty can ever be (before exponential factor)
		if x.Cmp(params.MinimumDifficulty) < 0 {
			x.Set(params.MinimumDifficulty)
		}
		// calculate a fake block number for the ice-age delay
		// Specification: https://eips.ethereum.org/EIPS/eip-1234
		fakeBlockNumber := new(big.Int)
		if parent.Number.Cmp(bombDelayFromParent) >= 0 {
			fakeBlockNumber = fakeBlockNumber.Sub(parent.Number, bombDelayFromParent)
		}
		// for the exponential factor
		periodCount := fakeBlockNumber
		periodCount.Div(periodCount, expDiffPeriod)

		// the exponential factor, commonly referred to as "the bomb"
		// diff = diff + 2^(periodCount - 2)
		if periodCount.Cmp(big1) > 0 {
			y.Sub(periodCount, big2)
			y.Exp(big2, y, nil)
			x.Add(x, y)
		}
		return x
	}
}

// calcDifficultyHomestead is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time given the
// parent block's time and difficulty. The calculation uses the Homestead rules.
func calcDifficultyHomestead(time uint64, parent *types.Header) *big.Int {
	// https://github.com/ethereum/EIPs/blob/master/EIPS/eip-2.md
	// algorithm:
	// diff = (parent_diff +
	//         (parent_diff / 2048 * max(1 - (block_timestamp - parent_timestamp) // 10, -99))
	//        ) + 2^(periodCount - 2)

	bigTime := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).SetUint64(parent.Time)

	// holds intermediate values to make the algo easier to read & audit
	x := new(big.Int)
	y := new(big.Int)

	// 1 - (block_timestamp - parent_timestamp) // 10
	x.Sub(bigTime, bigParentTime)
	x.Div(x, big10)
	x.Sub(big1, x)

	// max(1 - (block_timestamp - parent_timestamp) // 10, -99)
	if x.Cmp(bigMinus99) < 0 {
		x.Set(bigMinus99)
	}
	// (parent_diff + parent_diff // 2048 * max(1 - (block_timestamp - parent_timestamp) // 10, -99))
	y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
	x.Mul(y, x)
	x.Add(parent.Difficulty, x)

	// minimum difficulty can ever be (before exponential factor)
	if x.Cmp(params.MinimumDifficulty) < 0 {
		x.Set(params.MinimumDifficulty)
	}
	// for the exponential factor
	periodCount := new(big.Int).Add(parent.Number, big1)
	periodCount.Div(periodCount, expDiffPeriod)

	// the exponential factor, commonly referred to as "the bomb"
	// diff = diff + 2^(periodCount - 2)
	if periodCount.Cmp(big1) > 0 {
		y.Sub(periodCount, big2)
		y.Exp(big2, y, nil)
		x.Add(x, y)
	}
	return x
}

// calcDifficultyFrontier is the difficulty adjustment algorithm. It returns the
// difficulty that a new block should have when created at time given the parent
// block's time and difficulty. The calculation uses the Frontier rules.
func calcDifficultyFrontier(time uint64, parent *types.Header) *big.Int {
	diff := new(big.Int)
	adjust := new(big.Int).Div(parent.Difficulty, params.DifficultyBoundDivisor)
	bigTime := new(big.Int)
	bigParentTime := new(big.Int)

	bigTime.SetUint64(time)
	bigParentTime.SetUint64(parent.Time)

	if bigTime.Sub(bigTime, bigParentTime).Cmp(params.DurationLimit) < 0 {
		diff.Add(parent.Difficulty, adjust)
	} else {
		diff.Sub(parent.Difficulty, adjust)
	}
	if diff.Cmp(params.MinimumDifficulty) < 0 {
		diff.Set(params.MinimumDifficulty)
	}

	periodCount := new(big.Int).Add(parent.Number, big1)
	periodCount.Div(periodCount, expDiffPeriod)
	if periodCount.Cmp(big1) > 0 {
		// diff = diff + 2^(periodCount - 2)
		expDiff := periodCount.Sub(periodCount, big2)
		expDiff.Exp(big2, expDiff, nil)
		diff.Add(diff, expDiff)
		diff = math.BigMax(diff, params.MinimumDifficulty)
	}
	return diff
}

// Exported for fuzzing
var FrontierDifficultyCalculator = calcDifficultyFrontier
var HomesteadDifficultyCalculator = calcDifficultyHomestead
var DynamicDifficultyCalculator = makeDifficultyCalculator

// verifySeal checks whether a block satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (ethash *Ethash) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, fulldag bool) error {
	// If we're running a fake PoW, accept any seal as valid
	if ethash.config.PowMode == ModeFake || ethash.config.PowMode == ModeFullFake {
		time.Sleep(ethash.fakeDelay)
		if ethash.fakeFail == header.Number.Uint64() {
			return errInvalidPoW
		}
		return nil
	}
	// If we're running a shared PoW, delegate verification to it
	if ethash.shared != nil {
		return ethash.shared.verifySeal(chain, header, fulldag)
	}
	// Ensure that we have a valid difficulty for the block
	if header.Difficulty.Sign() <= 0 {
		return errInvalidDifficulty
	}
	// Recompute the digest and PoW values
	number := header.Number.Uint64()

	var (
		digest []byte
		result []byte
	)
	// If fast-but-heavy PoW verification was requested, use an ethash dataset
	if fulldag {
		dataset := ethash.dataset(number, true)
		if dataset.generated() {
			digest, result = hashimotoFull(dataset.dataset, ethash.SealHash(header).Bytes(), header.Nonce.Uint64())

			// Datasets are unmapped in a finalizer. Ensure that the dataset stays alive
			// until after the call to hashimotoFull so it's not unmapped while being used.
			runtime.KeepAlive(dataset)
		} else {
			// Dataset not yet generated, don't hang, use a cache instead
			fulldag = false
		}
	}
	// If slow-but-light PoW verification was requested (or DAG not yet ready), use an ethash cache
	if !fulldag {
		cache := ethash.cache(number)

		size := datasetSize(number, ethashEpochLength)
		if ethash.config.PowMode == ModeTest {
			size = 32 * 1024
		}
		digest, result = hashimotoLight(size, cache.cache, ethash.SealHash(header).Bytes(), header.Nonce.Uint64())

		// Caches are unmapped in a finalizer. Ensure that the cache stays alive
		// until after the call to hashimotoLight so it's not unmapped while being used.
		runtime.KeepAlive(cache)
	}
	// Verify the calculated values against the ones provided in the header
	if !bytes.Equal(header.MixDigest[:], digest) {
		return errInvalidMixDigest
	}
	target := new(big.Int).Div(two256, header.Difficulty)
	if new(big.Int).SetBytes(result).Cmp(target) > 0 {
		return errInvalidPoW
	}
	return nil
}

// VerifyEthashTxSeal checks whether a offline mining satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (ethash *Ethash) VerifyEthashTxSeal(tx *types.Transaction, fulldag bool) error {
	// If we're running a fake PoW, accept any seal as valid
	if ethash.config.PowMode == ModeFake || ethash.config.PowMode == ModeFullFake {
		time.Sleep(ethash.fakeDelay)
		return nil
	}
	// If we're running a shared PoW, delegate verification to it
	if ethash.shared != nil {
		return ethash.shared.VerifyEthashTxSeal(tx, fulldag)
	}

	// Recompute the digest and PoW values, using tx nonce and the number of dataset
	number := tx.Nonce()
	var (
		digest []byte
		result []byte
	)
	// If fast-but-heavy PoW verification was requested, use an ethash dataset
	if fulldag {
		dataset := ethash.dataset(number, true)
		if dataset.generated() {
			digest, result = hashimotoFull(dataset.dataset, tx.SealHash().Bytes(), tx.PowNonce())

			// Datasets are unmapped in a finalizer. Ensure that the dataset stays alive
			// until after the call to hashimotoFull so it's not unmapped while being used.
			runtime.KeepAlive(dataset)
		} else {
			// Dataset not yet generated, don't hang, use a cache instead
			fulldag = false
		}
	}
	// If slow-but-light PoW verification was requested (or DAG not yet ready), use an ethash cache
	if !fulldag {
		cache := ethash.cache(number)

		size := datasetSize(number, ethashEpochLength)
		if ethash.config.PowMode == ModeTest {
			size = 32 * 1024
		}
		digest, result = hashimotoLight(size, cache.cache, tx.SealHash().Bytes(), tx.PowNonce())

		// Caches are unmapped in a finalizer. Ensure that the cache stays alive
		// until after the call to hashimotoLight so it's not unmapped while being used.
		runtime.KeepAlive(cache)
	}
	// Verify the calculated values against the ones provided in the header
	mixDigest := tx.MixDigest()
	if !bytes.Equal(mixDigest[:], digest) {
		return errInvalidMixDigest
	}
	target := new(big.Int).Div(two256, tx.Difficulty())
	if new(big.Int).SetBytes(result).Cmp(target) > 0 {
		return errInvalidPoW
	}
	return nil
}

// Prepare implements consensus.Engine, initializing the difficulty field of a
// header to conform to the ethash protocol. The changes are done inline.
func (ethash *Ethash) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = ethash.CalcDifficulty(chain, header.Time, parent)
	return nil
}

// Finalize implements consensus.Engine, accumulating the block and uncle rewards.
func (ethash *Ethash) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal) {
	// Accumulate any block and uncle rewards
	accumulateRewards(chain.Config(), state, header)
}

// FinalizeAndAssemble implements consensus.Engine, accumulating the block and
// uncle rewards, setting the final state and assembling the block.
func (ethash *Ethash) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt, withdrawals []*types.Withdrawal) (*types.Block, error) {
	if len(withdrawals) > 0 {
		return nil, errors.New("ethash does not support withdrawals")
	}
	// Finalize block
	ethash.Finalize(chain, header, state, txs, uncles, nil)

	// Assign the final state root to header.
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(header, txs, uncles, receipts, trie.NewStackTrie(nil)), nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (ethash *Ethash) SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()

	enc := []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		// header.Time is intentionally excluded: PoW 2.0 stamps the real seal time
		// into the block after the nonce is found (see ethash.mine), and every
		// miner must agree on one SealHash before the timestamp exists. Difficulty
		// is derived from sealed ancestors (CalcDifficultyPoW2), not from Time, so
		// excluding it here is safe.
		header.Extra,
	}
	if header.BaseFee != nil {
		enc = append(enc, header.BaseFee)
	}
	if header.WithdrawalsHash != nil {
		panic("withdrawal hash set on ethash")
	}
	rlp.Encode(hasher, enc)
	hasher.Sum(hash[:0])
	return hash
}

// accumulateRewards credits the coinbase of the given block with the mining
// reward from the capped halving schedule. The full reward goes to the miner;
// there is no foundation cut in PoW 2.0.
func accumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header) {
	if !config.IsCanxium(header.Number) {
		return
	}
	state.AddBalance(header.Coinbase, blockReward(header.Number))
}

// blockReward returns the mining reward for the block at the given number,
// following the halving schedule (Pow2InitialReward, halved every
// Pow2HalvingEraBlocks) and clamped so that cumulative emission never exceeds
// Pow2MiningCap. The HydroBlock fork no longer affects the reward.
func blockReward(number *big.Int) *big.Int {
	n := number.Uint64()
	era := n / Pow2HalvingEraBlocks
	reward := new(big.Int).Rsh(Pow2InitialReward, uint(era))

	// Enforce the hard cap: never mint past Pow2MiningCap.
	remaining := new(big.Int).Sub(Pow2MiningCap, cumulativeEmission(n))
	if remaining.Sign() <= 0 {
		return new(big.Int)
	}
	if reward.Cmp(remaining) > 0 {
		return remaining
	}
	return reward
}

// cumulativeEmission returns the total block reward minted by every block
// strictly before block n (i.e. blocks 1..n-1). Block 0 is the genesis block
// and is never mined, so its era-0 reward is excluded.
func cumulativeEmission(n uint64) *big.Int {
	total := new(big.Int)
	if n <= 1 {
		return total
	}
	last := n - 1 // highest already-minted block
	lastEra := last / Pow2HalvingEraBlocks

	// Full eras below the current one: Pow2HalvingEraBlocks blocks each.
	eraBlocks := new(big.Int).SetUint64(Pow2HalvingEraBlocks)
	for era := uint64(0); era < lastEra; era++ {
		r := new(big.Int).Rsh(Pow2InitialReward, uint(era))
		total.Add(total, r.Mul(r, eraBlocks))
	}
	// Partial current era: blocks [lastEra*eraBlocks, last].
	count := new(big.Int).SetUint64(last - lastEra*Pow2HalvingEraBlocks + 1)
	r := new(big.Int).Rsh(Pow2InitialReward, uint(lastEra))
	total.Add(total, r.Mul(r, count))

	// The loop/partial sum counts block 0 (genesis) in era 0; subtract it.
	return total.Sub(total, Pow2InitialReward)
}
