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
	"github.com/ethereum/go-ethereum/log"
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

	ethashEpochLength = uint64(30000) // Ethash epoch length, used to calculate DAG size
)

var (
	ErrInvalidMiningType = errors.New("invalid mining transaction type")
)

var (
	errInvalidDifficulty    = errors.New("non-positive difficulty")
	errDifficultyUnderValue = errors.New("mining transaction difficulty under value")
	errInvalidMiningTxValue = errors.New("invalid mining transaction value")
	errInvalidMiningEpoch   = errors.New("invalid mining epoch, offline mining tx nonce must be < 30000 after Beryllium fork")
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
	// We only support ethash offline mining for the MiningTxType
	if tx.Type() == types.MiningTxType {
		return engine.VerifyEthashTxSeal(tx, false)
	}
	return ErrInvalidMiningType
}

// MiningEpoch returns the epoch number for a given mining transaction
func MiningEpoch(tx *types.Transaction) uint64 {
	if tx.Type() == types.CrossMiningTxType && tx.AuxPoW() != nil {
		return tx.AuxPoW().Epoch()
	}
	// for the MiningTxType, the epoch number is calculated base on the nonce and only ethash is supported
	if tx.Type() == types.MiningTxType {
		return GetEthashEpochNumber(tx.Nonce())
	}
	return 0
}

// VerifyMiningTxs verifies a batch of mining transactions
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func VerifyMiningTxs(config *params.ChainConfig, engine consensus.Engine, txs types.Transactions, block *types.Header) <-chan int64 {
	// If we're running a full engine faking, accept any input as valid
	result := make(chan int64, 1)
	defer close(result)
	// After Beryllium fork, we only allow one epoch per mining algorithm in a block
	if config.IsBerylliumTime(block.Time) {
		epochMap := make(map[crosschain.PoWAlgorithm]uint64)
		for _, tx := range txs {
			if !tx.IsMiningTx() {
				continue
			}
			epoch := MiningEpoch(tx)
			if lastEpoch, ok := epochMap[tx.Algorithm()]; ok && lastEpoch != epoch {
				log.Error("Multiple mining epochs in single block", "algorithm", tx.Algorithm(), "epoch1", lastEpoch, "epoch2", epoch)
				result <- -1
				return result
			}
			epochMap[tx.Algorithm()] = epoch
		}
	}
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
	// After Beryllium fork, we don't allow legacy mining tx have nonce > 30,000
	if config.IsBerylliumTime(block.Time) && tx.Nonce() >= ethashEpochLength {
		return errInvalidMiningEpoch
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

// GetEthashEpochNumber return the ethash epoch number for a given block number
func GetEthashEpochNumber(blockNumber uint64) uint64 {
	return blockNumber / ethashEpochLength
}
