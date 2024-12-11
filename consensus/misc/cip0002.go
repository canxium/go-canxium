package misc

import (
	"bytes"
	"errors"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// Merge mining protocol constants.
	LitecoinMergeMiningSubsidy                  = uint64(28000000000) // 28 gwei per difficulty ~ 1.4 CAU/50 MH difficulty
	LitecoinMergeMiningSubsidyReductionInterval = uint64(63000000)    // 63000000 second ~ 420,000 litecoin blocks ~ 2 years
	LitecoinMergeMiningPeriod                   = uint64(252460800)   // 8 years
)

var (
	// make sure miner set the correct input data for the transaction
	CanxiumMergeMiningTxDataLength = 36

	// mergeMining(address) method
	// TODO: Update method signature
	CanxiumMergeMiningTxDataMethod = common.Hex2Bytes("eedc3c83000000000000000000000000")

	big0   = big.NewInt(0)
	bigOne = big.NewInt(1)

	mainPowMax = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	errInvalidDifficulty      = errors.New("non-positive difficulty")
	errDifficultyUnderValue   = errors.New("mining transaction difficulty under value")
	errInvalidMiningTxChain   = errors.New("invalid merge mining transaction parent chain")
	errInvalidMiningTxValue   = errors.New("invalid merge mining transaction value")
	ErrInvalidMiningReceiver  = errors.New("invalid merge mining transaction receiver")
	ErrInvalidMiningSender    = errors.New("invalid merge mining transaction sender")
	ErrInvalidMiningInput     = errors.New("invalid merge mining transaction input data")
	ErrInvalidMiningAlgorithm = errors.New("invalid merge mining transaction algorithm")

	ErrInvalidMergePoW      = errors.New("invalid merge mining transaction proof of work")
	ErrInvalidMergeCoinbase = errors.New("invalid merge mining transaction coinbase")
)

// verifyMergeMiningTxSeal checks whether a merge mining satisfies the PoW difficulty requirements,
func VerifyMergeMiningTxSeal(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if !isSupportedParentChain(config, tx, block.Time) {
		return errInvalidMiningTxChain
	}

	// Ensure the receiver is the mining smart contract
	if tx.To() == nil || *tx.To() != config.MiningContract {
		return ErrInvalidMiningReceiver
	}
	// Ensure that we have a valid difficulty for the transaction
	if tx.Difficulty().Sign() <= 0 {
		return errInvalidDifficulty
	}

	minDiff := MergeMiningMinDifficulty(tx.MergeProof().Chain())
	if tx.Difficulty().Cmp(minDiff) < 0 {
		return errDifficultyUnderValue
	}

	// TODO: Prevent relay attack, using the same block for multi transaction to earn more CAU
	// IMPORTANT!

	// Make sure they call the correct method of contract: mining(address)
	if len(tx.Data()) != CanxiumMergeMiningTxDataLength || !bytes.Equal(CanxiumMergeMiningTxDataMethod, tx.Data()[0:16]) {
		return ErrInvalidMiningInput
	}

	// Ensure value is valid: reward * difficulty
	chainForkTime := MergeMiningForkTime(config, tx.MergeProof().Chain())
	subsidy := MergeMiningSubsidy(tx.MergeProof().Chain(), chainForkTime, block.Time)
	value := new(big.Int).Mul(subsidy, tx.Difficulty())
	if tx.Value().Cmp(value) != 0 {
		return errInvalidMiningTxValue
	}

	proof := tx.MergeProof()
	if proof.VerifyPoW() != nil {
		return ErrInvalidMergePoW
	}

	if !proof.VerifyCoinbase() {
		return ErrInvalidMergeCoinbase
	}

	_, err := proof.GetMinerAddress()
	if err != nil {
		return err
	}

	return nil
}

// Calculate merge mining reward base
func MergeMiningForkTime(config *params.ChainConfig, parentChain types.ParentChain) uint64 {
	switch parentChain {
	case types.KaspaChain:
		return *config.HeliumTime
	}

	return math.MaxUint64
}

// Calculate merge mining reward base
func MergeMiningSubsidy(parentChain types.ParentChain, forkTime uint64, time uint64) *big.Int {
	if time < forkTime {
		return big0
	}

	switch parentChain {
	// case types.LitecoinChain:
	// 	if LitecoinMergeMiningSubsidyReductionInterval == 0 {
	// 		return big.NewInt(int64(LitecoinMergeMiningSubsidy))
	// 	}

	// 	// 8 years period check
	// 	if (time - forkTime) > LitecoinMergeMiningPeriod {
	// 		return big0
	// 	}

	// 	// Equivalent to: baseSubsidy / 2^(height/subsidyHalvingInterval)
	// 	return big.NewInt(int64(LitecoinMergeMiningSubsidy >> uint64((time-forkTime)/LitecoinMergeMiningSubsidyReductionInterval)))
	case types.KaspaChain:
		return big0
	}

	return big0
}

// MergeMiningMinDifficulty return the minimum difficulty for each chain
func MergeMiningMinDifficulty(parentChain types.ParentChain) *big.Int {
	switch parentChain {
	case types.KaspaChain:
		return params.KaspaMergeMiningMinDifficulty
	}

	return mainPowMax
}

// isSupportedParentChain check if this fork support for this parent chain
func isSupportedParentChain(config *params.ChainConfig, tx *types.Transaction, blockTime uint64) bool {
	if config.IsHelium(blockTime) && tx.MergeProof().Chain() == types.KaspaChain {
		return true
	}

	return false
}
