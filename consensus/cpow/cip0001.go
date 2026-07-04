package cpow

import (
	"bytes"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// Maximum number of mining transaction per block
	MaxMiningTransactionPerBlock = 10
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
	errMaxMiningTxsExceeded = errors.New("max mining transactions exceeded in block")
)

// VerifyMiningTx verifies a mining transaction
func VerifyMiningTx(engine consensus.Engine, config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if !tx.IsMiningTx() {
		if IsUnauthorizedCrossMiningTx(config, tx) {
			return ErrUnauthorizedCrossMiningTx
		}

		return nil
	}

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

// verifyTxMiningSeal checks whether a mining transaction satisfies the proof of work requirement of the mining protocol, including ethash, kkHeavyHash and more
func VerifyMiningTxSeal(engine consensus.Engine, config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if tx.Type() == types.CrossMiningTxType {
		return VerifyCrossMiningSeal(tx)
	}
	// We only support ethash offline mining for the MiningTxType
	if tx.Type() == types.MiningTxType {
		return engine.VerifyEthashTxSeal(tx, false)
	}
	return ErrInvalidMiningType
}

// verifyTxMiningBasic checks whether an ethash offline mining satisfies the basic requirement of the offline mining protocol
func verifyOfflineMiningBasic(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	// We don't allow legacy mining tx have nonce > 30,000
	if tx.Nonce() >= ethashEpochLength {
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
