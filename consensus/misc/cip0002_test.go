package misc

import (
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"
)

// function to calculate kaspa cross mining base reward
func kaspaCrossMiningRewardAlgorithm() {
	// Constants
	initialReward := 0.5                              // Start with 0.5 CAU
	dailyDecayFactor0 := math.Pow(0.1, 1.0/(0.5*30))  // Daily decay factor for the first phase
	dailyDecayFactor := math.Pow(0.25, 1.0/(2.0*30))  // Daily decay factor for the first phase
	dailyDecayFactor2 := math.Pow(0.6, 1.0/(17.0*30)) // Daily decay factor for the second phase
	days := 180 * 30                                  // Total number of days (180 months)

	// Slice to store the base reward for each day
	baseRewards := make([]float64, days)

	// Calculate the base reward for each day
	for day := 0; day < days; day++ {
		if day < 3 {
			baseRewards[day] = initialReward * math.Pow(dailyDecayFactor0, float64(day))
		} else if day <= 103 {
			baseRewards[day] = 0.27 * math.Pow(dailyDecayFactor, float64(day))
		} else {
			baseRewards[day] = 0.0275 * math.Pow(dailyDecayFactor2, float64(day))
		}
	}

	// Print the table header
	fmt.Println("Month,Base Reward (CAU/Block)")
	// From day 3 onwards, set the reward to the monthly average
	month := 0
	for monthStart := 3; monthStart < days; monthStart += 30 {
		monthEnd := monthStart + 30
		if monthEnd > days {
			monthEnd = days
		}

		// Calculate the average reward for the month
		sum := 0.0
		for day := monthStart; day < monthEnd; day++ {
			sum += baseRewards[day]
		}
		avgReward := sum / float64(monthEnd-monthStart)

		// Assign the average reward to all days in the month
		for day := monthStart; day < monthEnd; day++ {
			baseRewards[day] = avgReward
		}

		fmt.Printf("%5d,%.10f\n", month, avgReward)
		month++
	}
}

func TestKaspaCrossMiningReward(t *testing.T) {
	// Test parameters
	difficulty := big.NewInt(1000000000000000000) // Example difficulty value

	// Calculate reward
	rewardDay0 := kaspaCrossMiningReward(difficulty, 1704067200, 1704067300)
	if rewardDay0.Cmp(big.NewInt(600000000000000000)) != 0 {
		t.Errorf("Day 0: Reward %s should equal %d", rewardDay0.String(), 600000000000000000)
	}

	rewardDay1 := kaspaCrossMiningReward(difficulty, 1704067200, 1704157200)
	if rewardDay1.Cmp(big.NewInt(400000000000000000)) != 0 {
		t.Errorf("Day 1: Reward %s should equal %d", rewardDay1.String(), 400000000000000000)
	}

	rewardDay2 := kaspaCrossMiningReward(difficulty, 1704067200, 1704240000)
	if rewardDay2.Cmp(big.NewInt(200000000000000000)) != 0 {
		t.Errorf("Day 2: Reward %s should equal %d", rewardDay2.String(), 200000000000000000)
	}

	rewardDay3 := kaspaCrossMiningReward(difficulty, 1704067200, 1704326400)
	if rewardDay3.Cmp(big.NewInt(183829000000000000)) != 0 {
		t.Errorf("Day 3: Reward %s should equal %d", rewardDay3.String(), 183829000000000000)
	}

	rewardDay4 := kaspaCrossMiningReward(difficulty, 1704067200, 1704421800)
	if rewardDay4.Cmp(big.NewInt(183829000000000000)) != 0 {
		t.Errorf("Day 3: Reward %s should equal %d", rewardDay4.String(), 183829000000000000)
	}

	rewardDay33 := kaspaCrossMiningReward(difficulty, 1704067200, 1706920742)
	if rewardDay33.Cmp(big.NewInt(91915000000000000)) != 0 {
		t.Errorf("Day 33: Reward %s should equal %d", rewardDay33.String(), 91915000000000000)
	}

	rewardDay34 := kaspaCrossMiningReward(difficulty, 1704067200, 1707009800)
	if rewardDay34.Cmp(big.NewInt(91915000000000000)) != 0 {
		t.Errorf("Day 34: Reward %s should equal %d", rewardDay34.String(), 91915000000000000)
	}

	rewardDay110 := kaspaCrossMiningReward(difficulty, 1704067200, 1713574900)
	if rewardDay110.Cmp(big.NewInt(25868000000000000)) != 0 {
		t.Errorf("Day 110: Reward %s should equal %d", rewardDay110.String(), 25868000000000000)
	}

	rewardDay1735 := kaspaCrossMiningReward(difficulty, 1704067200, 1853974200)
	if rewardDay1735.Cmp(big.NewInt(4875000000000000)) != 0 {
		t.Errorf("Day 1735: Reward %s should equal %d", rewardDay1735.String(), 4875000000000000)
	}

	rewardDay1736 := kaspaCrossMiningReward(difficulty, 1704067200, 1854060600)
	if rewardDay1736.Cmp(big.NewInt(4875000000000000)) != 0 {
		t.Errorf("Day 1735: Reward %s should equal %d", rewardDay1736.String(), 4875000000000000)
	}

	start := uint64(1704067200)
	step := uint64(86399) // 16 hours
	for i := uint64(0); i < 5405; i++ {
		reward := kaspaCrossMiningReward(difficulty, start, start+step*i)
		dayNum, month := timePassedSinceFork(start, start+step*i)
		fmt.Printf("%d,%d,%s\n", dayNum, month, reward.String())
	}
}

func TestTimePassedSinceFork(t *testing.T) {
	tests := []struct {
		name      string
		forkTime  uint64
		time      uint64
		expDays   uint64
		expMonths uint64
	}{
		{"Same time", 1704067200, 1704067200, 0, 0},
		{"One day after fork", 1704067200, 1704153600, 1, 0},
		{"One month after fork", 1704067200, 1706659200, 30, 1},
		{"One year after fork", 1704067200, 1735689600, 366, 12},
		{"Five years after fork", 1704067200, 1869801600, 1918, 63},
		{"Boundary case: just before a day passes", 1704067200, 1704153599, 0, 0},
		{"Boundary case: just before a month passes", 1704067200, 1706659199, 29, 0},
		{"Large gap: 15 years", 1704067200, 2177443200, 5478, 182},
		{"Before fork (invalid case)", 1704067200, 1704060000, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			days, months := timePassedSinceFork(tc.forkTime, tc.time)
			if days != tc.expDays || months != tc.expMonths {
				t.Errorf("%s: expected (%d, %d) but got (%d, %d)", tc.name, tc.expDays, tc.expMonths, days, months)
			}
		})
	}
}

// Example usage with real-time timestamps
func ExampleTimePassedSinceFork() {
	forkTime := uint64(1704067200)   // Example: January 1, 2024, 00:00 UTC
	now := uint64(time.Now().Unix()) // Current timestamp
	days, months := timePassedSinceFork(forkTime, now)
	println("Days since fork:", days, "Months since fork:", months)
}
