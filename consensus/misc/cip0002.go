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

var (
	// make sure miner set the correct input data for the transaction
	CanxiumMergeMiningTxDataLength = 36

	// mergeMining(address) method
	CanxiumMergeMiningTxDataMethod = common.Hex2Bytes("f7b78a49")

	big0   = big.NewInt(0)
	bigOne = big.NewInt(1)

	mainPowMax = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

	// Kaspa merge mining reward constants
	KaspaMergePhraseOneReward   = big.NewFloat(0.5)   // 0.5 wei per difficulty
	KaspaMergePhraseTwoReward   = big.NewFloat(0.27)  // 0.27 wei per difficulty
	KaspaMergePhraseThreeReward = big.NewFloat(0.022) // 0.022 wei per difficulty
	KaspaPhaseTwoDayNum         = uint64(3)
	KaspaPhaseThreeDayNum       = uint64(115)
	KaspaPhaseFourEndDayNum     = uint64(5404)

	// Kaspa Merge Mining
	KaspaDecayFactorOne   = powFloat64(0.1, 1.0/(0.5*30))  // Daily decay factor for the first phase
	KaspaDecayFactorTwo   = powFloat64(0.25, 1.0/(2.0*30)) // Daily decay factor for the second phase
	KaspaDecayFactorThree = powFloat64(0.4, 1.0/(17.0*30)) // Daily decay factor for the third phase
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	ErrInvalidDifficulty         = errors.New("non-positive difficulty")
	ErrDifficultyUnderValue      = errors.New("mining transaction difficulty under value")
	ErrInvalidMiningTimeLine     = errors.New("invalid merge mining transaction timeline")
	ErrInvalidMiningBlockTime    = errors.New("invalid merge mining block timestamp")
	ErrInvalidMiningTxValue      = errors.New("invalid merge mining transaction value")
	ErrInvalidMiningReceiver     = errors.New("invalid merge mining transaction receiver")
	ErrInvalidMiningSender       = errors.New("invalid merge mining transaction sender")
	ErrInvalidMiningInput        = errors.New("invalid merge mining transaction input data")
	ErrInvalidMiningAlgorithm    = errors.New("invalid merge mining transaction algorithm")
	ErrInvalidMiningInputAddress = errors.New("invalid merge mining transaction receiver address and block's miner")

	ErrInvalidMergeNilBlock = errors.New("invalid merge mining block, block is nil")
	ErrInvalidMergeBlock    = errors.New("invalid merge mining block")
	ErrInvalidMergePoW      = errors.New("invalid merge mining transaction proof of work")
	ErrInvalidMergeCoinbase = errors.New("invalid merge mining transaction coinbase")
)

// verifyMergeMiningTxSeal checks whether a merge mining satisfies the PoW difficulty requirements,
func VerifyMergeMiningTxSeal(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if tx.MergeProof() == nil {
		return ErrInvalidMergeNilBlock
	}
	if !tx.MergeProof().IsValidBlock() {
		return ErrInvalidMergeBlock
	}
	if !isSupportedMergeMining(config, tx, block.Time) {
		return ErrInvalidMiningTimeLine
	}
	// Ensure the receiver is the mining smart contract
	if tx.To() == nil || *tx.To() != config.MiningContract {
		return ErrInvalidMiningReceiver
	}
	// Ensure that we have a valid difficulty for the transaction
	if tx.Difficulty().Sign() <= 0 {
		return ErrInvalidDifficulty
	}
	mergeBlock := tx.MergeProof()
	minDiff := MergeMiningMinDifficulty(config, mergeBlock.Chain())
	if tx.Difficulty().Cmp(minDiff) < 0 {
		return ErrDifficultyUnderValue
	}
	// Make sure they call the correct method of contract: mining(address)
	if len(tx.Data()) != CanxiumMergeMiningTxDataLength || !bytes.Equal(CanxiumMergeMiningTxDataMethod, tx.Data()[:4]) {
		return ErrInvalidMiningInput
	}
	// Ensure value is valid: reward * difficulty
	chainForkTime := MergeMiningForkTime(config, mergeBlock.Chain())
	// Check block's timestamp
	timestamp := mergeBlock.Timestamp()
	if timestamp < chainForkTime {
		return ErrInvalidMiningBlockTime
	}
	reward := MergeMiningReward(mergeBlock, chainForkTime, block.Time)
	if tx.Value().Cmp(reward) != 0 {
		return ErrInvalidMiningTxValue
	}

	if err := mergeBlock.VerifyPoW(); err != nil {
		return ErrInvalidMergePoW
	}
	if !mergeBlock.VerifyCoinbase() {
		return ErrInvalidMergeCoinbase
	}
	miner, err := mergeBlock.GetMinerAddress()
	if err != nil {
		return err
	}

	receiverBytes := tx.Data()[4:36]
	receiver := common.BytesToAddress(receiverBytes)
	if receiver != miner {
		return ErrInvalidMiningInputAddress
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

// Calculate merge mining reward
func MergeMiningReward(mergeBlock types.MergeBlock, forkTime uint64, time uint64) *big.Int {
	if time < forkTime {
		return big0
	}

	switch mergeBlock.Chain() {
	case types.KaspaChain:
		dayNum := dayNumberBetweenTime(forkTime, time)
		reward, _ := kaspaMergeMiningReward(mergeBlock.Difficulty(), uint64(dayNum))
		return reward
	}

	return big0
}

// MergeMiningMinDifficulty return the minimum difficulty for each chain
func MergeMiningMinDifficulty(config *params.ChainConfig, parentChain types.ParentChain) *big.Int {
	switch parentChain {
	case types.KaspaChain:
		return config.MergeMining.MinimumKaspaDifficulty
	}

	return mainPowMax
}

// isSupportedMergeMining check if this timeline support for this parent chain
func isSupportedMergeMining(config *params.ChainConfig, tx *types.Transaction, blockTime uint64) bool {
	if tx.MergeProof().Chain() == types.KaspaChain {
		if !config.IsHelium(blockTime) {
			return false
		}

		dayNum := dayNumberBetweenTime(*config.HeliumTime, blockTime)
		return dayNum <= KaspaPhaseFourEndDayNum
	}

	return false
}

// kaspaMergeMiningReward calculate reward for the difficulty of a kaspa block
func kaspaMergeMiningReward(difficulty *big.Int, dayNum uint64) (*big.Int, big.Accuracy) {
	baseReward := new(big.Float)
	reward := new(big.Float)
	dayBig := big.NewFloat(float64(dayNum))

	if dayNum < KaspaPhaseTwoDayNum {
		baseReward.Mul(KaspaMergePhraseOneReward, powBig(KaspaDecayFactorOne, dayBig))
	} else if dayNum <= KaspaPhaseThreeDayNum {
		baseReward.Mul(KaspaMergePhraseTwoReward, powBig(KaspaDecayFactorTwo, dayBig))
	} else if dayNum <= KaspaPhaseFourEndDayNum {
		baseReward.Mul(KaspaMergePhraseThreeReward, powBig(KaspaDecayFactorThree, dayBig))
	} else {
		return big0, 0
	}

	difficultyInFloat := new(big.Float).SetInt(difficulty)
	reward.Mul(difficultyInFloat, baseReward)
	return reward.Int(nil)
}

func dayNumberBetweenTime(forkTime, time uint64) uint64 {
	// Ensure forkTime is not greater than time to avoid negative day numbers
	if time < forkTime {
		return 0
	}

	// Calculate the difference in seconds and convert to days
	secondsInADay := uint64(86400)
	dayNumber := (time - forkTime) / secondsInADay

	return uint64(dayNumber)
}

// powFloat calculates base^exponent for float64
func powFloat64(base, exponent float64) *big.Float {
	return new(big.Float).SetFloat64(math.Pow(base, exponent))
}

// powBig calculates base^exponent for big.Float
func powBig(base, exponent *big.Float) *big.Float {
	exp, _ := exponent.Float64()
	res, _ := base.Float64()

	return new(big.Float).SetFloat64(math.Pow(res, exp))
}
