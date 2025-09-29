package cpow

import (
	"bytes"
	"errors"
	"math/big"
	"runtime"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	crosschain "github.com/ethereum/go-ethereum/core/types/cross-chain"
	"github.com/ethereum/go-ethereum/params"
)

var (
	CanxiumRewardPerHash = big.NewInt(13) // Reward in wei per difficulty hash for successfully mining upward from Canxium

	// offline mining
	CanxiumMaxTransactionReward = big.NewInt(4250)
	CanxiumMiningReduceBlock    = big.NewInt(432000) // Offline mining reward reduce 11.76% every 432000 blocks
	CanxiumMiningReducePeriod   = big.NewInt(48)     // Max 48 months
	CanxiumMiningPeriodPercent  = big.NewInt(8842)

	// make sure miner set the correct input data for the transaction
	CanxiumMiningTxDataLength = 36
	// mining(address) method
	CanxiumMiningTxDataMethod = common.Hex2Bytes("eedc3c83000000000000000000000000")
)

var (
	ErrInvalidMiningType = errors.New("invalid mining transaction type")
)

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
	errInvalidEngine        = errors.New("invalid consensus engine")
)

// VerifyMiningTx verifies a mining transaction
func VerifyMiningTx(engine consensus.Engine, config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if err := VerifyMiningTxBasic(config, tx, block); err != nil {
		return err
	}
	return VerifyMiningTxSeal(engine, config, tx, block)
}

// verifyCrossMiningTxSeal checks whether a cross mining satisfies the basic requirements of the cross-chain mining protocol
func VerifyMiningTxBasic(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if tx.Type() == types.CrossMiningTxType {
		return verifyCrossMiningBasic(config, tx, block)
	}
	if tx.Type() == types.MiningTxType {
		return verifyOfflineMiningBasic(config, tx, block)
	}
	return ErrInvalidMiningType
}

// verifyTxMiningSeal checks whether a mining transaction satisfies the proof of work requirement of the mining protocol, including ethash, kkHeavyHash, kawpow and more
func VerifyMiningTxSeal(engine consensus.Engine, config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if tx.Type() == types.CrossMiningTxType {
		if tx.Algorithm() == crosschain.KawPoWAlgorithm {
			return engine.VerifyKawPowTxSeal(tx)
		}
		return VerifyCrossMiningSeal(tx)
	}
	if tx.Type() == types.MiningTxType && IsEthashAlgorithm(config, block.Time, tx.Algorithm()) {
		return engine.VerifyEthashTxSeal(tx, false)
	}
	return ErrInvalidMiningType
}

// VerifyMiningTxs verifies a batch of mining transactions
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func VerifyMiningTxs(config *params.ChainConfig, engine consensus.Engine, txs types.Transactions, block *types.Header) <-chan int64 {
	// If we're running a full engine faking, accept any input as valid
	result := make(chan int64, 1)
	defer close(result)
	if len(txs) == 0 {
		result <- 0
		return result
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(txs) < workers {
		workers = len(txs)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs       = make(chan int)
		done         = make(chan int, workers)
		errors       = make([]error, len(txs))
		numMiningTxs = int64(0)
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				if !txs[index].IsMiningTx() {
					if IsUnauthorizedCrossMiningTx(config, txs[index]) {
						errors[index] = ErrUnauthorizedCrossMiningTx
					} else {
						errors[index] = nil
					}
					done <- index
					continue
				}

				atomic.AddInt64(&numMiningTxs, 1)
				errors[index] = VerifyMiningTx(engine, config, txs[index], block)
				done <- index
			}
		}()
	}

	sealCh := make(chan int64, 1)
	go func() {
		defer close(inputs)
		defer close(sealCh)
		var (
			in, out = 0, 0
			checked = make([]bool, len(txs))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(txs) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					if errors[out] != nil {
						sealCh <- -1
						// if any of txs have error, return.
						return
					}

					if out == len(txs)-1 {
						sealCh <- atomic.LoadInt64(&numMiningTxs)
						return
					}
				}
			}
		}
	}()
	return sealCh
}

// verifyTxMiningBasic checks whether an ethash offline mining satisfies the basic requirement of the offline mining protocol
func verifyOfflineMiningBasic(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if !config.IsHydro(block.Number) {
		return types.ErrTxTypeNotSupported
	}
	// Ensure the receiver is the mining smart contract
	if tx.To() == nil || *tx.To() != config.MiningContract {
		return ErrInvalidMiningReceiver
	}
	// Ensure that we have a valid difficulty for the transaction
	if tx.Difficulty().Sign() <= 0 {
		return errInvalidDifficulty
	}
	if tx.Difficulty().Cmp(config.Ethash.MinimumDifficulty) < 0 {
		return errDifficultyUnderValue
	}
	// Ensure signer and from are same to avoid pow relay attack
	signer := types.MakeSigner(config, block.Number)
	from, err := types.Sender(signer, tx)
	if err != nil {
		return err
	}
	if from != tx.From() {
		return ErrInvalidMiningSender
	}

	// Make sure they call the correct method of contract: mining(address)
	if len(tx.Data()) != CanxiumMiningTxDataLength || !bytes.Equal(CanxiumMiningTxDataMethod, tx.Data()[0:16]) {
		return ErrInvalidMiningInput
	}

	// Ensure value is valid: reward * difficulty
	subsidy := TransactionMiningSubsidy(config, block.Number)
	value := new(big.Int).Mul(subsidy, tx.Difficulty())
	if tx.Value().Cmp(value) != 0 {
		return errInvalidMiningTxValue
	}
	return nil
}

// Calculate offline mining reward base on block number
func TransactionMiningSubsidy(config *params.ChainConfig, block *big.Int) *big.Int {
	if !config.IsHydro(block) {
		return big0
	}
	blockPassed := new(big.Int).Sub(block, config.HydroBlock)
	period := new(big.Int).Div(blockPassed, CanxiumMiningReduceBlock)
	if period.Cmp(big0) == 0 {
		return CanxiumMaxTransactionReward
	}

	// reduce mining reward for max 48 period
	if period.Cmp(CanxiumMiningReducePeriod) >= 0 {
		return CanxiumRewardPerHash
	}

	exp := new(big.Int).Exp(CanxiumMiningPeriodPercent, period, nil)
	percentage := new(big.Int).Exp(big.NewInt(10000), period, nil)
	periodReward := new(big.Int).Mul(CanxiumMaxTransactionReward, exp)
	subsidy := new(big.Int).Div(periodReward, percentage)
	return subsidy
}

// Check to see if an algorithm number is presenting for ethash
// Because before the Helium fork, we didn't verify the tx.Algorithm number, so all number = ethash, but we want it is 1.
// Some miner set a different number (2, 3, 4) and the transaction is excuted success, now we have to defined all that number to be ethash
func IsEthashAlgorithm(config *params.ChainConfig, blockTime uint64, algorithm crosschain.PoWAlgorithm) bool {
	// before helium fork, all number is ethash
	if !config.IsHelium(blockTime) {
		return true
	}

	// after helium fork, only number 1
	switch algorithm {
	case crosschain.EthashAlgorithm:
		return true
	}

	return false
}
