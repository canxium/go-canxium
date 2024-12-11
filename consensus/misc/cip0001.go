package misc

import (
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

var (
	CanxiumRewardPerHash = big.NewInt(250) // Reward in wei per difficulty hash for successfully mining upward from Canxium

	// offline mining
	CanxiumMaxTransactionReward = big.NewInt(4250)
	CanxiumMiningReduceBlock    = big.NewInt(432000) // Offline mining reward reduce 11.76% every 432000 blocks
	CanxiumMiningReducePeriod   = big.NewInt(24)     // Max 24 months
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

	// reduce mining reward for max 24 period
	if period.Cmp(CanxiumMiningReducePeriod) >= 0 {
		return CanxiumRewardPerHash
	}

	exp := new(big.Int).Exp(CanxiumMiningPeriodPercent, period, nil)
	percentage := new(big.Int).Exp(big.NewInt(10000), period, nil)
	periodReward := new(big.Int).Mul(CanxiumMaxTransactionReward, exp)
	subsidy := new(big.Int).Div(periodReward, percentage)
	return subsidy
}
