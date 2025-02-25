package misc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var (
	// make sure miner set the correct input data for the transaction
	CanxiumCrossMiningTxDataLength = 36

	// crossMining(address,uint16,uint256) method
	CanxiumCrossMiningTxDataMethod = common.Hex2Bytes("97b8f2fc")

	big0   = big.NewInt(0)
	bigOne = big.NewInt(1)

	mainPowMax = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

	// Max milliseconds from current time allowed for blocks, before they're considered future blocks
	allowedFutureBlockTimeMilliSeconds = uint64(900000)

	// Kaspa cross mining reward constants for mainnet
	KaspaPhaseTwoDayNum  = uint64(3)
	KaspaPhaseThreeMonth = uint64(141)

	// map from first 3 days to base reward
	KaspaCrossMiningIncentiveBaseRewards = [3]int64{600000, 400000, 200000}
	// map from month to base reward: wei per params.KaspaMinAcceptableDifficulty difficulty, default 1000000
	KaspaCrossMiningBaseRewards = [142]int64{183829, 91915, 45958, 25868, 23963, 23254, 22566, 21898, 21249, 20620, 20010, 19418, 18843, 18285, 17744, 17219, 16709, 16214, 15734, 15269, 14817, 14378, 13953, 13540, 13139, 12750, 12372, 12006, 11651, 11306, 10971, 10647, 10331, 10026, 9729, 9441, 9161, 8890, 8627, 8372, 8124, 7883, 7650, 7424, 7204, 6991, 6784, 6583, 6388, 6199, 6016, 5838, 5665, 5497, 5334, 5176, 5023, 4875, 4730, 4590, 4454, 4323, 4195, 4070, 3950, 3833, 3720, 3610, 3503, 3399, 3298, 3201, 3106, 3014, 2925, 2838, 2754, 2673, 2594, 2517, 2442, 2370, 2300, 2232, 2166, 2102, 2040, 1979, 1921, 1864, 1809, 1755, 1703, 1653, 1604, 1556, 1510, 1466, 1422, 1380, 1339, 1300, 1261, 1224, 1188, 1153, 1119, 1085, 1053, 1022, 992, 963, 934, 906, 880, 854, 828, 804, 780, 757, 735, 713, 692, 671, 651, 632, 613, 595, 578, 561, 544, 528, 512, 497, 482, 468, 454, 441, 428, 415, 403, 400}
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	ErrInvalidDifficulty         = errors.New("invalid cross mining transaction: non-positive difficulty")
	ErrDifficultyUnderValue      = errors.New("invalid cross mining transaction: difficulty under value")
	ErrInvalidMiningTimeLine     = errors.New("invalid cross mining transaction: cross mining not supported yet")
	ErrInvalidMiningBlockTime    = errors.New("invalid cross mining transaction: invalid block timestamp")
	ErrInvalidMiningTxValue      = errors.New("invalid cross mining transaction: invalid value")
	ErrInvalidMiningReceiver     = errors.New("invalid cross mining transaction: invalid receiver")
	ErrInvalidMiningSender       = errors.New("invalid cross mining transaction: invalid sender")
	ErrInvalidMiningInput        = errors.New("invalid cross mining transaction: invalid input data")
	ErrInvalidMiningAlgorithm    = errors.New("invalid cross mining transaction: invalid algorithm")
	ErrInvalidMiningInputAddress = errors.New("invalid cross mining transaction: invalid receiver address and block's miner")
	ErrInvalidFutureBlock        = errors.New("invalid cross mining transaction: block in the future")

	ErrInvalidNilBlock        = errors.New("invalid cross mining transaction: block is nil")
	ErrInvalidCrossChainBlock = errors.New("invalid cross mining transaction: invalid block")
	ErrInvalidMergePoW        = errors.New("invalid cross mining transaction: invalid proof of work")
	ErrInvalidMergeCoinbase   = errors.New("invalid cross mining transaction: invalid coinbase")

	ErrUnauthorizedCrossMiningTx = errors.New("interact with crossChainMining method of mining contract from normal transaction is not allowed")
)

// verifyCrossMiningTxSeal checks whether a cross mining satisfies the PoW difficulty requirements,
func VerifyCrossMiningTxSeal(config *params.ChainConfig, tx *types.Transaction, block *types.Header) error {
	if tx.AuxPoW() == nil {
		return ErrInvalidNilBlock
	}
	if !tx.AuxPoW().IsValidBlock() {
		return ErrInvalidCrossChainBlock
	}
	if !isSupportedCrossMining(config, tx, block.Time) {
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
	crossBlock := tx.AuxPoW()
	minDiff := CrossMiningMinDifficulty(config, crossBlock.Chain())
	if tx.Difficulty().Cmp(minDiff) < 0 {
		return ErrDifficultyUnderValue
	}
	// Check block's timestamp
	chainForkTimeMilli := CrossMiningForkTimeMilli(config, crossBlock.Chain())
	timestamp := crossBlock.Timestamp()
	if timestamp < chainForkTimeMilli {
		return ErrInvalidMiningBlockTime
	}
	blockTimeMilli := block.Time * 1000
	if timestamp > blockTimeMilli+allowedFutureBlockTimeMilliSeconds {
		return ErrInvalidFutureBlock
	}
	// Ensure value is valid: reward * difficulty
	chainForkTime := CrossMiningForkTime(config, crossBlock.Chain())
	reward := CrossMiningReward(crossBlock, chainForkTime, block.Time)
	if tx.Value().Cmp(reward) != 0 {
		return ErrInvalidMiningTxValue
	}

	if err := crossBlock.VerifyPoW(); err != nil {
		return ErrInvalidMergePoW
	}
	if !crossBlock.VerifyCoinbase() {
		return ErrInvalidMergeCoinbase
	}
	miner, err := crossBlock.GetMinerAddress()
	if err != nil {
		return err
	}

	// Make sure they call the correct method of contract, with the correct args
	inputData := buildCrossMiningTxInput(crossBlock.Chain(), miner, timestamp)
	if !bytes.Equal(inputData, tx.Data()) {
		return ErrInvalidMiningInput
	}

	return nil
}

// IsUnauthorizedCrossMiningTx check if a normal transaction is interacting with the crossChainMininig method of the mining contract
// this is not allowed action, because the crossChainMining method is a special method, it stored the block timestamp on the contract
// bad man can call it and set the timestamp to infinity.
func IsUnauthorizedCrossMiningTx(config *params.ChainConfig, tx *types.Transaction) bool {
	// check if the transaction is interacting with mining contract, crossChainMining method, then only allow transaction types.CrossMiningTxType
	if tx.To() != nil && *tx.To() == config.MiningContract {
		if len(tx.Data()) >= 4 && bytes.Equal(CanxiumCrossMiningTxDataMethod, tx.Data()[:4]) {
			if tx.Type() != types.CrossMiningTxType {
				return true
			}
		}
	}

	return false
}

// CrossMiningForkTimeMilli Return fork time, in millisecond to compare the merge block time
func CrossMiningForkTimeMilli(config *params.ChainConfig, parentChain types.CrossChain) uint64 {
	forkTime := CrossMiningForkTime(config, parentChain)
	if forkTime != math.MaxUint64 {
		return forkTime * 1000
	}

	return math.MaxUint64
}

// CrossMiningForkTime Return fork time, in second to calculate mining reward
func CrossMiningForkTime(config *params.ChainConfig, parentChain types.CrossChain) uint64 {
	switch parentChain {
	case types.KaspaChain:
		return *config.HeliumTime
	}

	return math.MaxUint64
}

// Calculate cross mining reward
func CrossMiningReward(crossBlock types.CrossChainBlock, forkTime uint64, time uint64) *big.Int {
	if time < forkTime {
		return big0
	}

	switch crossBlock.Chain() {
	case types.KaspaChain:
		reward := kaspaCrossMiningReward(crossBlock.Difficulty(), forkTime, time)
		return reward
	}

	return big0
}

// CrossMiningMinDifficulty return the minimum difficulty for each chain
func CrossMiningMinDifficulty(config *params.ChainConfig, parentChain types.CrossChain) *big.Int {
	switch parentChain {
	case types.KaspaChain:
		return config.CrossMining.MinimumKaspaDifficulty
	}

	return mainPowMax
}

// isSupportedCrossMining check if this timeline support for this parent chain
func isSupportedCrossMining(config *params.ChainConfig, tx *types.Transaction, blockTime uint64) bool {
	if tx.AuxPoW().Chain() == types.KaspaChain {
		return config.IsHelium(blockTime)
	}

	return false
}

// kaspaCrossMiningReward calculate reward for the difficulty of a kaspa block
func kaspaCrossMiningReward(difficulty *big.Int, forkTime uint64, time uint64) *big.Int {
	day, month := timePassedSinceFork(forkTime, time)
	baseReward := new(big.Int)

	if day < KaspaPhaseTwoDayNum {
		baseReward.SetInt64(KaspaCrossMiningIncentiveBaseRewards[day])
	} else if month < KaspaPhaseThreeMonth {
		baseReward.SetInt64(KaspaCrossMiningBaseRewards[month])
	} else {
		baseReward.SetInt64(KaspaCrossMiningBaseRewards[KaspaPhaseThreeMonth])
	}

	// Multiply difficulty * baseReward (per 1000000 hash) / 1000000
	reward := new(big.Int).Mul(difficulty, baseReward)
	return reward.Div(reward, big.NewInt(1000000))
}

func timePassedSinceFork(forkTime, time uint64) (dayNum uint64, month uint64) {
	// Ensure forkTime is not greater than time to avoid negative day numbers
	if time < forkTime {
		return 0, 0
	}

	// Calculate the difference in seconds and convert to days and month
	dayNum = (time - forkTime) / 86400
	month = (time - forkTime) / 2592000
	return
}

func buildCrossMiningTxInput(chain types.CrossChain, address common.Address, timestamp uint64) []byte {
	// Check input data, match: method_receiver_chain_timestamp
	paddedAddress := common.LeftPadBytes(address.Bytes(), 32)
	// Timestamp (uint256) is padded to 32 bytes
	timestampBig := new(big.Int).SetUint64(timestamp)
	timestampPadded := make([]byte, 32)
	timestampBig.FillBytes(timestampPadded)
	// Convert the chain ID to a hexadecimal value and pad it to 32 bytes
	chainHex := fmt.Sprintf("%04x", chain)                             // Convert uint16 to a 4-character hex string
	chainPadded, _ := hex.DecodeString(fmt.Sprintf("%064s", chainHex)) // Pad with leading zeros to 32 bytes
	var data []byte
	data = append(data, CanxiumCrossMiningTxDataMethod...)
	data = append(data, paddedAddress...)
	data = append(data, chainPadded...)
	data = append(data, timestampPadded...)
	return data
}
