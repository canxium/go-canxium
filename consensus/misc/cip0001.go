package misc

import (
	"math/big"

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
)

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
