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
	crosschain "github.com/ethereum/go-ethereum/core/types/cross-chain"
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

	// mainPowMaxInLithiumFork is the highest proof of work value a Kaspa block can
	// have for the lithium fork. It is the value 2^256 / 512. So left kaspa block is allowed to be transfered to canxium chain.
	// EST: 10 BPS => 615937 blocks per year will be trans
	targetPoWInLithiumFork = new(big.Int).Rsh(big.NewInt(1).Lsh(big.NewInt(1), 256), 0) // 2^256
	maxPoWInLithiumFork    = targetPoWInLithiumFork.Div(targetPoWInLithiumFork, big.NewInt(512))

	// Max milliseconds from current time allowed for blocks, before they're considered future blocks
	allowedFutureBlockTimeMilliSeconds             = uint64(12000)
	allowedFutureBlockTimeMilliSecondsPostLithitum = uint64(300000) // 5 minutes

	// Kaspa cross mining reward constants for mainnet
	KaspaPhaseTwoDayNum  = uint64(3)
	KaspaPhaseThreeMonth = uint64(141)
	// Raven cross mining reward constants for mainnet
	RavenPhaseTwoMonth = uint64(138)

	// map from first 3 days to base reward
	KaspaCrossMiningIncentiveBaseRewards = [3]int64{600000, 400000, 200000}
	// map from month to base reward: wei per params.KaspaMinAcceptableDifficulty difficulty, default 1000000
	KaspaCrossMiningBaseRewards        = [142]int64{183829, 91915, 45958, 25868, 23963, 23254, 22566, 21898, 21249, 20620, 20010, 19418, 18843, 18285, 17744, 17219, 16709, 16214, 15734, 15269, 14817, 14378, 13953, 13540, 13139, 12750, 12372, 12006, 11651, 11306, 10971, 10647, 10331, 10026, 9729, 9441, 9161, 8890, 8627, 8372, 8124, 7883, 7650, 7424, 7204, 6991, 6784, 6583, 6388, 6199, 6016, 5838, 5665, 5497, 5334, 5176, 5023, 4875, 4730, 4590, 4454, 4323, 4195, 4070, 3950, 3833, 3720, 3610, 3503, 3399, 3298, 3201, 3106, 3014, 2925, 2838, 2754, 2673, 2594, 2517, 2442, 2370, 2300, 2232, 2166, 2102, 2040, 1979, 1921, 1864, 1809, 1755, 1703, 1653, 1604, 1556, 1510, 1466, 1422, 1380, 1339, 1300, 1261, 1224, 1188, 1153, 1119, 1085, 1053, 1022, 992, 963, 934, 906, 880, 854, 828, 804, 780, 757, 735, 713, 692, 671, 651, 632, 613, 595, 578, 561, 544, 528, 512, 497, 482, 468, 454, 441, 428, 415, 403, 400}
	KaspaCrossMiningLithiumBaseRewards = [142]int64{94120448, 47060480, 23530496, 13244416, 12269056, 11906048, 11553792, 11211776, 10879488, 10557440, 10245120, 9942016, 9647616, 9361920, 9084928, 8816128, 8555008, 8301568, 8055808, 7817728, 7586304, 7361536, 7143936, 6932480, 6727168, 6528000, 6334464, 6147072, 5965312, 5788672, 5617152, 5451264, 5289472, 5133312, 4981248, 4833792, 4690432, 4551680, 4417024, 4286464, 4159488, 4036096, 3916800, 3801088, 3688448, 3579392, 3473408, 3370496, 3270656, 3173888, 3080192, 2989056, 2900480, 2814464, 2731008, 2650112, 2571776, 2496000, 2421760, 2350080, 2280448, 2213376, 2147840, 2083840, 2022400, 1962496, 1904640, 1848320, 1793536, 1740288, 1688576, 1638912, 1590272, 1543168, 1497600, 1453056, 1410048, 1368576, 1328128, 1288704, 1250304, 1213440, 1177600, 1142784, 1108992, 1076224, 1044480, 1013248, 983552, 954368, 926208, 898560, 871936, 846336, 821248, 796672, 773120, 750592, 728064, 706560, 685568, 665600, 645632, 626688, 608256, 590336, 572928, 555520, 539136, 523264, 507904, 493056, 478208, 463872, 450560, 437248, 423936, 411648, 399360, 387584, 376320, 365056, 354304, 343552, 333312, 323584, 313856, 304640, 295936, 287232, 278528, 270336, 262144, 254464, 246784, 239616, 232448, 225792, 219136, 212480, 206336, 204800}
	// map from month to base reward: wei per 1 difficulty
	RavenCrossMiningRewards = [139]int64{16666666666667, 8333333333333, 5000000000000, 4741666666667, 4393333333333, 4263333333333, 4136666666667, 4015000000000, 3895000000000, 3780000000000, 3668333333333, 3560000000000, 3455000000000, 3351666666667, 3253333333333, 3156666666667, 3063333333333, 2973333333333, 2885000000000, 2800000000000, 2716666666667, 2636666666667, 2558333333333, 2481666666667, 2408333333333, 2336666666667, 2268333333333, 2201666666667, 2136666666667, 2073333333333, 2011666666667, 1951666666667, 1893333333333, 1838333333333, 1783333333333, 1730000000000, 1680000000000, 1630000000000, 1581666666667, 1535000000000, 1490000000000, 1445000000000, 1401666666667, 1361666666667, 1320000000000, 1281666666667, 1243333333333, 1206666666667, 1171666666667, 1136666666667, 1103333333333, 1070000000000, 1038333333333, 1008333333333, 978333333333, 948333333333, 921666666667, 893333333333, 866666666667, 841666666667, 816666666667, 791666666667, 768333333333, 746666666667, 723333333333, 703333333333, 681666666667, 661666666667, 641666666667, 623333333333, 605000000000, 586666666667, 570000000000, 553333333333, 536666666667, 520000000000, 505000000000, 490000000000, 475000000000, 461666666667, 448333333333, 435000000000, 421666666667, 408333333333, 396666666667, 385000000000, 373333333333, 363333333333, 351666666667, 341666666667, 331666666667, 321666666667, 311666666667, 303333333333, 293333333333, 285000000000, 276666666667, 268333333333, 260000000000, 253333333333, 245000000000, 238333333333, 231666666667, 225000000000, 218333333333, 211666666667, 205000000000, 198333333333, 193333333333, 186666666667, 181666666667, 176666666667, 171666666667, 166666666667, 161666666667, 156666666667, 151666666667, 146666666667, 143333333333, 138333333333, 135000000000, 130000000000, 126666666667, 123333333333, 120000000000, 115000000000, 111666666667, 108333333333, 106666666667, 103333333333, 100000000000, 96666666667, 93333333333, 91666666667, 88333333333, 85000000000, 83333333333, 80000000000, 55000000000}
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
	ErrInvalidBlockPoWHash    = errors.New("invalid cross mining transaction: invalid block PoW hash")

	ErrUnauthorizedCrossMiningTx = errors.New("interact with crossChainMining method of mining contract from normal transaction is not allowed")
)

func isValidKaspaBlockHash(hashHex string) (bool, error) {
	hashBytes, err := hex.DecodeString(hashHex)
	if err != nil {
		return false, err
	}

	hashInt := new(big.Int).SetBytes(hashBytes)
	return hashInt.Cmp(maxPoWInLithiumFork) == -1, nil
}

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
	// Ensure block hash is valid for kaspa chain after the fork to reduce block rate
	if config.IsLithium(block.Time) && tx.AuxPoW().Chain() == crosschain.KaspaChain {
		valid, err := isValidKaspaBlockHash(tx.AuxPoW().BlockHash())
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalidBlockPoWHash
		}
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
	futureBlockTime := allowedFutureBlockTimeMilliSeconds
	if config.IsLithium(block.Time) {
		futureBlockTime = allowedFutureBlockTimeMilliSecondsPostLithitum
	}
	if timestamp > blockTimeMilli+futureBlockTime {
		return ErrInvalidFutureBlock
	}
	// Ensure value is valid: reward * difficulty
	chainForkTime := CrossMiningForkTime(config, crossBlock.Chain())
	reward := CrossMiningReward(config.IsLithium(block.Time), crossBlock, chainForkTime, block.Time)
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
func CrossMiningForkTimeMilli(config *params.ChainConfig, parentChain crosschain.CrossChain) uint64 {
	forkTime := CrossMiningForkTime(config, parentChain)
	if forkTime != math.MaxUint64 {
		return forkTime * 1000
	}

	return math.MaxUint64
}

// CrossMiningForkTime Return fork time, in second to calculate mining reward
func CrossMiningForkTime(config *params.ChainConfig, parentChain crosschain.CrossChain) uint64 {
	switch parentChain {
	case crosschain.KaspaChain:
		return *config.HeliumTime
	case crosschain.RavenChain:
		return *config.BerylliumTime // Same fork time as Kaspa for now
	}

	return math.MaxUint64
}

// Calculate cross mining reward
func CrossMiningReward(isLithiumFork bool, crossBlock crosschain.CrossChainBlock, forkTime uint64, time uint64) *big.Int {
	if time < forkTime {
		return big0
	}

	switch crossBlock.Chain() {
	case crosschain.KaspaChain:
		reward := kaspaCrossMiningReward(isLithiumFork, crossBlock.Difficulty(), forkTime, time)
		return reward
	case crosschain.RavenChain:
		reward := ravenCrossMiningReward(crossBlock.Difficulty(), forkTime, time)
		return reward
	}

	return big0
}

// CrossMiningMinDifficulty return the minimum difficulty for each chain
func CrossMiningMinDifficulty(config *params.ChainConfig, parentChain crosschain.CrossChain) *big.Int {
	switch parentChain {
	case crosschain.KaspaChain:
		return config.CrossMining.MinimumKaspaDifficulty
	case crosschain.RavenChain:
		return params.RavenMinAcceptableDifficulty
	}

	return mainPowMax
}

// isSupportedCrossMining check if this timeline support for this parent chain
func isSupportedCrossMining(config *params.ChainConfig, tx *types.Transaction, blockTime uint64) bool {
	if tx.AuxPoW().Chain() == crosschain.KaspaChain {
		return config.IsHelium(blockTime)
	}
	if tx.AuxPoW().Chain() == crosschain.RavenChain {
		return config.IsBerylliumTime(blockTime)
	}

	return false
}

// kaspaCrossMiningReward calculate reward for the difficulty of a kaspa block
func kaspaCrossMiningReward(isLithiumFork bool, difficulty *big.Int, forkTime uint64, time uint64) *big.Int {
	day, month := timePassedSinceFork(forkTime, time)
	baseReward := new(big.Int)
	baseRewards := KaspaCrossMiningBaseRewards
	if isLithiumFork {
		baseRewards = KaspaCrossMiningLithiumBaseRewards
	}

	if day < KaspaPhaseTwoDayNum {
		baseReward.SetInt64(KaspaCrossMiningIncentiveBaseRewards[day])
	} else if month < KaspaPhaseThreeMonth {
		baseReward.SetInt64(baseRewards[month])
	} else {
		baseReward.SetInt64(baseRewards[KaspaPhaseThreeMonth])
	}

	// Multiply difficulty * baseReward (per 1000000 hash) / 1000000
	reward := new(big.Int).Mul(difficulty, baseReward)
	return reward.Div(reward, params.KaspaMinAcceptableDifficulty)
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

func buildCrossMiningTxInput(chain crosschain.CrossChain, address common.Address, timestamp uint64) []byte {
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

// ravenCrossMiningReward calculate reward for the difficulty of a raven block
func ravenCrossMiningReward(difficulty *big.Int, forkTime uint64, time uint64) *big.Int {
	_, month := timePassedSinceFork(forkTime, time)
	baseReward := new(big.Int)

	if month >= RavenPhaseTwoMonth {
		baseReward.SetInt64(RavenCrossMiningRewards[RavenPhaseTwoMonth])
	} else {
		baseReward.SetInt64(RavenCrossMiningRewards[month])
	}

	// Multiply difficulty * baseReward (per 1000000 hash) / 1000000
	reward := new(big.Int).Mul(difficulty, baseReward)
	reward.Div(reward, big.NewInt(1000000))

	return reward
}
