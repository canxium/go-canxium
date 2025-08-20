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
	rewardDay0 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1704067300)
	if rewardDay0.Cmp(big.NewInt(600000000000000000)) != 0 {
		t.Errorf("Day 0: Reward %s should equal %d", rewardDay0.String(), 600000000000000000)
	}

	rewardDay1 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1704157200)
	if rewardDay1.Cmp(big.NewInt(400000000000000000)) != 0 {
		t.Errorf("Day 1: Reward %s should equal %d", rewardDay1.String(), 400000000000000000)
	}

	rewardDay2 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1704240000)
	if rewardDay2.Cmp(big.NewInt(200000000000000000)) != 0 {
		t.Errorf("Day 2: Reward %s should equal %d", rewardDay2.String(), 200000000000000000)
	}

	rewardDay3 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1704326400)
	if rewardDay3.Cmp(big.NewInt(183829000000000000)) != 0 {
		t.Errorf("Day 3: Reward %s should equal %d", rewardDay3.String(), 183829000000000000)
	}

	rewardDay4 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1704421800)
	if rewardDay4.Cmp(big.NewInt(183829000000000000)) != 0 {
		t.Errorf("Day 3: Reward %s should equal %d", rewardDay4.String(), 183829000000000000)
	}

	rewardDay33 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1706920742)
	if rewardDay33.Cmp(big.NewInt(91915000000000000)) != 0 {
		t.Errorf("Day 33: Reward %s should equal %d", rewardDay33.String(), 91915000000000000)
	}

	rewardDay34 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1707009800)
	if rewardDay34.Cmp(big.NewInt(91915000000000000)) != 0 {
		t.Errorf("Day 34: Reward %s should equal %d", rewardDay34.String(), 91915000000000000)
	}

	rewardDay110 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1713574900)
	if rewardDay110.Cmp(big.NewInt(25868000000000000)) != 0 {
		t.Errorf("Day 110: Reward %s should equal %d", rewardDay110.String(), 25868000000000000)
	}

	rewardDay1735 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1853974200)
	if rewardDay1735.Cmp(big.NewInt(4875000000000000)) != 0 {
		t.Errorf("Day 1735: Reward %s should equal %d", rewardDay1735.String(), 4875000000000000)
	}

	rewardDay1736 := kaspaCrossMiningReward(false, difficulty, 1704067200, 1854060600)
	if rewardDay1736.Cmp(big.NewInt(4875000000000000)) != 0 {
		t.Errorf("Day 1735: Reward %s should equal %d", rewardDay1736.String(), 4875000000000000)
	}

	// Calculate reward
	shiftRewardDay0 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1704067300)
	expectedRewardDay0 := new(big.Int)
	expectedRewardDay0.SetString("600000000000000000", 10) // Use SetString for large values
	if shiftRewardDay0.Cmp(expectedRewardDay0) != 0 {
		t.Errorf("Day 0: Reward %s should equal %s", rewardDay0.String(), expectedRewardDay0.String())
	}

	shiftRewardDay1 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1704157200)
	expectedShiftRewardDay1 := new(big.Int)
	expectedShiftRewardDay1.SetString("400000000000000000", 10) // Use SetString for large values
	if shiftRewardDay1.Cmp(expectedShiftRewardDay1) != 0 {
		t.Errorf("Day 1: Reward %s should equal %s", shiftRewardDay1.String(), expectedShiftRewardDay1.String())
	}

	shiftRewardDay2 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1704240000)
	expectedShiftRewardDay2 := new(big.Int)
	expectedShiftRewardDay2.SetString("200000000000000000", 10) // Use SetString for large values
	if shiftRewardDay2.Cmp(expectedShiftRewardDay2) != 0 {
		t.Errorf("Day 2: Reward %s should equal %s", shiftRewardDay2.String(), expectedShiftRewardDay2.String())
	}

	shiftRewardDay3 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1704326400)
	expectedShiftRewardDay3 := new(big.Int)
	expectedShiftRewardDay3.SetString("183829000000000000000", 10) // Use SetString for large values
	if shiftRewardDay3.Cmp(expectedShiftRewardDay3) != 0 {
		t.Errorf("Day 3: Reward %s should equal %s", shiftRewardDay3.String(), expectedShiftRewardDay3.String())
	}

	shiftRewardDay4 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1704421800)
	expectedShiftRewardDay4 := new(big.Int)
	expectedShiftRewardDay4.SetString("183829000000000000000", 10) // Use SetString for large values
	if shiftRewardDay4.Cmp(expectedShiftRewardDay4) != 0 {
		t.Errorf("Day 4: Reward %s should equal %s", shiftRewardDay4.String(), expectedShiftRewardDay4.String())
	}

	shiftRewardDay33 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1706920742)
	expectedShiftRewardDay33 := new(big.Int)
	expectedShiftRewardDay33.SetString("91915000000000000000", 10) // Use SetString for large values
	if shiftRewardDay33.Cmp(expectedShiftRewardDay33) != 0 {
		t.Errorf("Day 33: Reward %s should equal %s", shiftRewardDay33.String(), expectedShiftRewardDay33.String())
	}

	shiftRewardDay34 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1707009800)
	expectedShiftRewardDay34 := new(big.Int)
	expectedShiftRewardDay34.SetString("91915000000000000000", 10) // Use SetString for large values
	if shiftRewardDay34.Cmp(expectedShiftRewardDay34) != 0 {
		t.Errorf("Day 34: Reward %s should equal %s", shiftRewardDay34.String(), expectedShiftRewardDay34.String())
	}

	shiftRewardDay110 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1713574900)
	expectedShiftRewardDay110 := new(big.Int)
	expectedShiftRewardDay110.SetString("25868000000000000000", 10) // Use SetString for large values
	if shiftRewardDay110.Cmp(expectedShiftRewardDay110) != 0 {
		t.Errorf("Day 110: Reward %s should equal %s", shiftRewardDay110.String(), expectedShiftRewardDay110.String())
	}

	shiftRewardDay1735 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1853974200)
	expectedShiftRewardDay1735 := new(big.Int)
	expectedShiftRewardDay1735.SetString("4875000000000000000", 10) // Use SetString for large values
	if shiftRewardDay1735.Cmp(expectedShiftRewardDay1735) != 0 {
		t.Errorf("Day 1735: Reward %s should equal %s", shiftRewardDay1735.String(), expectedShiftRewardDay1735.String())
	}

	shiftRewardDay1736 := kaspaCrossMiningReward(true, difficulty, 1704067200, 1854060600)
	expectedShiftRewardDay1736 := new(big.Int)
	expectedShiftRewardDay1736.SetString("4875000000000000000", 10) // Use SetString for large values
	if shiftRewardDay1736.Cmp(expectedShiftRewardDay1736) != 0 {
		t.Errorf("Day 1736: Reward %s should equal %s", shiftRewardDay1736.String(), expectedShiftRewardDay1736.String())
	}

	start := uint64(1704067200)
	step := uint64(86399) // 16 hours
	for i := uint64(0); i < 5405; i++ {
		reward := kaspaCrossMiningReward(false, difficulty, start, start+step*i)
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

// TestRavenCrossMiningReward tests the Raven cross-mining reward calculation
// for a 60,000 difficulty block across different months
func TestRavenCrossMiningReward(t *testing.T) {
	// Test difficulty: 60,000
	difficulty := big.NewInt(60000)

	// Fork time: January 1, 2024 (example)
	forkTime := uint64(1704067200) // January 1, 2024 00:00:00 UTC

	// Expected rewards for 60kH difficulty block (in CAU tokens)
	expectedRewards := []float64{
		1.0,    // Month 0
		0.5,    // Month 1
		0.3,    // Month 2
		0.2845, // Month 3
		0.2636, // Month 4
		0.2558, // Month 5
		0.2482, // Month 6
		0.2409, // Month 7
		0.2337, // Month 8
		0.2268, // Month 9
		0.2201, // Month 10
		0.2136, // Month 11
		0.2073, // Month 12
		0.2011, // Month 13
		0.1952, // Month 14
		0.1894, // Month 15
		0.1838, // Month 16
		0.1784, // Month 17
		0.1731, // Month 18
		0.168,  // Month 19
		0.163,  // Month 20
		0.1582, // Month 21
		0.1535, // Month 22
		0.1489, // Month 23
		0.1445, // Month 24
		0.1402, // Month 25
		0.1361, // Month 26
		0.1321, // Month 27
		0.1282, // Month 28
		0.1244, // Month 29
		0.1207, // Month 30
		0.1171, // Month 31
		0.1136, // Month 32
		0.1103, // Month 33
		0.107,  // Month 34
		0.1038, // Month 35
		0.1008, // Month 36
		0.0978, // Month 37
		0.0949, // Month 38
		0.0921, // Month 39
		0.0894, // Month 40
		0.0867, // Month 41
		0.0841, // Month 42
		0.0817, // Month 43
		0.0792, // Month 44
		0.0769, // Month 45
		0.0746, // Month 46
		0.0724, // Month 47
		0.0703, // Month 48
		0.0682, // Month 49
		0.0662, // Month 50
		0.0642, // Month 51
		0.0623, // Month 52
		0.0605, // Month 53
		0.0587, // Month 54
		0.0569, // Month 55
		0.0553, // Month 56
		0.0536, // Month 57
		0.052,  // Month 58
		0.0505, // Month 59
		0.049,  // Month 60
		0.0475, // Month 61
		0.0461, // Month 62
		0.0448, // Month 63
		0.0434, // Month 64
		0.0422, // Month 65
		0.0409, // Month 66
		0.0397, // Month 67
		0.0385, // Month 68
		0.0374, // Month 69
		0.0363, // Month 70
		0.0352, // Month 71
		0.0342, // Month 72
		0.0332, // Month 73
		0.0322, // Month 74
		0.0312, // Month 75
		0.0303, // Month 76
		0.0294, // Month 77
		0.0285, // Month 78
		0.0277, // Month 79
		0.0269, // Month 80
		0.0261, // Month 81
		0.0253, // Month 82
		0.0245, // Month 83
		0.0238, // Month 84
		0.0231, // Month 85
		0.0224, // Month 86
		0.0218, // Month 87
		0.0211, // Month 88
		0.0205, // Month 89
		0.0199, // Month 90
		0.0193, // Month 91
		0.0187, // Month 92
		0.0182, // Month 93
		0.0176, // Month 94
		0.0171, // Month 95
		0.0166, // Month 96
		0.0161, // Month 97
		0.0156, // Month 98
		0.0152, // Month 99
		0.0147, // Month 100
		0.0143, // Month 101
		0.0139, // Month 102
		0.0135, // Month 103
		0.0131, // Month 104
		0.0127, // Month 105
		0.0123, // Month 106
		0.0119, // Month 107
		0.0116, // Month 108
		0.0112, // Month 109
		0.0109, // Month 110
		0.0106, // Month 111
		0.0103, // Month 112
		0.01,   // Month 113
		0.0097, // Month 114
		0.0094, // Month 115
		0.0091, // Month 116
		0.0088, // Month 117
		0.0086, // Month 118
		0.0083, // Month 119
		0.0081, // Month 120
		0.0078, // Month 121
		0.0076, // Month 122
		0.0074, // Month 123
		0.0072, // Month 124
		0.0069, // Month 125
		0.0067, // Month 126
		0.0065, // Month 127
		0.0064, // Month 128
		0.0062, // Month 129
		0.006,  // Month 130
		0.0058, // Month 131
		0.0056, // Month 132
		0.0055, // Month 133
		0.0053, // Month 134
		0.0051, // Month 135
		0.005,  // Month 136
		0.0048, // Month 137
		0.0033, // Month 138+ (Phase 2)
	}

	// Test rewards for key months to verify the calculation
	testMonths := []int{0, 1, 2, 10, 50, 100, 137, 138}

	for _, month := range testMonths {
		// Calculate time for this month (30 days per month approximation)
		monthTime := forkTime + uint64(month)*2592000 // 30 days * 24 hours * 60 minutes * 60 seconds

		// Calculate the reward
		actualReward := ravenCrossMiningReward(difficulty, forkTime, monthTime)

		// Convert expected reward to wei (1 CAU = 10^18 wei)
		expectedRewardWei := new(big.Float).SetFloat64(expectedRewards[month])
		expectedRewardWei.Mul(expectedRewardWei, big.NewFloat(1e18))
		expectedRewardInt, _ := expectedRewardWei.Int(nil)

		// Allow for small rounding differences (within 0.1%)
		tolerance := new(big.Int).Div(expectedRewardInt, big.NewInt(1000)) // 0.1% tolerance
		diff := new(big.Int).Sub(actualReward, expectedRewardInt)
		if diff.Sign() < 0 {
			diff.Neg(diff)
		}

		if diff.Cmp(tolerance) > 0 {
			actualFloat := new(big.Float).SetInt(actualReward)
			actualFloat.Quo(actualFloat, big.NewFloat(1e18))
			t.Errorf("Month %d: Expected %.4f CAU, got %.4f CAU (diff: %s wei)",
				month, expectedRewards[month], actualFloat, diff.String())
		} else {
			actualFloat := new(big.Float).SetInt(actualReward)
			actualFloat.Quo(actualFloat, big.NewFloat(1e18))
			t.Logf("✅ Month %d: Expected %.4f CAU, got %.4f CAU",
				month, expectedRewards[month], actualFloat)
		}
	}
}

// TestRavenCrossMiningRewardPhases tests the phase transitions
func TestRavenCrossMiningRewardPhases(t *testing.T) {
	difficulty := big.NewInt(60000)
	forkTime := uint64(1704067200)

	t.Run("Phase 1 (Months 0-137)", func(t *testing.T) {
		// Test a few key months in phase 1
		testCases := []struct {
			month    int
			expected float64
		}{
			{0, 1.0},
			{1, 0.5},
			{50, 0.0662},
			{100, 0.0147},
			{137, 0.0048},
		}

		for _, tc := range testCases {
			monthTime := forkTime + uint64(tc.month)*2592000
			actualReward := ravenCrossMiningReward(difficulty, forkTime, monthTime)

			expectedRewardWei := new(big.Float).SetFloat64(tc.expected)
			expectedRewardWei.Mul(expectedRewardWei, big.NewFloat(1e18))
			expectedRewardInt, _ := expectedRewardWei.Int(nil)

			tolerance := new(big.Int).Div(expectedRewardInt, big.NewInt(1000))
			diff := new(big.Int).Sub(actualReward, expectedRewardInt)
			if diff.Sign() < 0 {
				diff.Neg(diff)
			}

			if diff.Cmp(tolerance) > 0 {
				t.Errorf("Phase 1 Month %d: reward mismatch", tc.month)
			} else {
				t.Logf("✅ Phase 1 Month %d: %.4f CAU", tc.month, tc.expected)
			}
		}
	})

	t.Run("Phase 2 (Month 138+)", func(t *testing.T) {
		// Test months in phase 2 - should all be 0.0033 CAU
		testMonths := []int{138, 139, 150, 200}
		expectedPhase2 := 0.0033

		for _, month := range testMonths {
			monthTime := forkTime + uint64(month)*2592000
			actualReward := ravenCrossMiningReward(difficulty, forkTime, monthTime)

			expectedRewardWei := new(big.Float).SetFloat64(expectedPhase2)
			expectedRewardWei.Mul(expectedRewardWei, big.NewFloat(1e18))
			expectedRewardInt, _ := expectedRewardWei.Int(nil)

			tolerance := new(big.Int).Div(expectedRewardInt, big.NewInt(1000))
			diff := new(big.Int).Sub(actualReward, expectedRewardInt)
			if diff.Sign() < 0 {
				diff.Neg(diff)
			}

			if diff.Cmp(tolerance) > 0 {
				t.Errorf("Phase 2 Month %d: reward mismatch", month)
			} else {
				t.Logf("✅ Phase 2 Month %d: %.4f CAU", month, expectedPhase2)
			}
		}
	})
}

// TestRavenCrossMiningRewardDifferentDifficulties tests rewards with different difficulties
func TestRavenCrossMiningRewardDifferentDifficulties(t *testing.T) {
	forkTime := uint64(1704067200)
	monthTime := forkTime + 0 // Month 0

	testCases := []struct {
		difficulty int64
		expected   float64 // Expected reward in CAU
	}{
		{60000, 1.0},   // Base case
		{120000, 2.0},  // Double difficulty = double reward
		{30000, 0.5},   // Half difficulty = half reward
		{6000, 0.1},    // 1/10 difficulty = 1/10 reward
		{600000, 10.0}, // 10x difficulty = 10x reward
	}

	for _, tc := range testCases {
		difficulty := big.NewInt(tc.difficulty)
		actualReward := ravenCrossMiningReward(difficulty, forkTime, monthTime)

		expectedRewardWei := new(big.Float).SetFloat64(tc.expected)
		expectedRewardWei.Mul(expectedRewardWei, big.NewFloat(1e18))
		expectedRewardInt, _ := expectedRewardWei.Int(nil)

		tolerance := new(big.Int).Div(expectedRewardInt, big.NewInt(1000))
		diff := new(big.Int).Sub(actualReward, expectedRewardInt)
		if diff.Sign() < 0 {
			diff.Neg(diff)
		}

		if diff.Cmp(tolerance) > 0 {
			actualFloat := new(big.Float).SetInt(actualReward)
			actualFloat.Quo(actualFloat, big.NewFloat(1e18))
			t.Errorf("Difficulty %d: Expected %.1f CAU, got %.4f CAU",
				tc.difficulty, tc.expected, actualFloat)
		} else {
			t.Logf("✅ Difficulty %d: %.1f CAU", tc.difficulty, tc.expected)
		}
	}
}

// TestRavenCrossMiningRewardEdgeCases tests edge cases
func TestRavenCrossMiningRewardEdgeCases(t *testing.T) {
	forkTime := uint64(1704067200)
	difficulty := big.NewInt(60000)

	t.Run("Time Before Fork", func(t *testing.T) {
		beforeFork := forkTime - 1000
		reward := ravenCrossMiningReward(difficulty, forkTime, beforeFork)
		if reward.Sign() != 0 {
			t.Errorf("Expected 0 reward before fork, got %s", reward.String())
		} else {
			t.Logf("✅ Correctly returns 0 for time before fork")
		}
	})

	t.Run("Time Exactly at Fork", func(t *testing.T) {
		reward := ravenCrossMiningReward(difficulty, forkTime, forkTime)
		if reward.Sign() <= 0 {
			t.Errorf("Expected positive reward at fork time, got %s", reward.String())
		} else {
			actualFloat := new(big.Float).SetInt(reward)
			actualFloat.Quo(actualFloat, big.NewFloat(1e18))
			t.Logf("✅ Reward at fork time: %.4f CAU", actualFloat)
		}
	})

	t.Run("Zero Difficulty", func(t *testing.T) {
		zeroDiff := big.NewInt(0)
		monthTime := forkTime + 2592000 // Month 1
		reward := ravenCrossMiningReward(zeroDiff, forkTime, monthTime)
		if reward.Sign() != 0 {
			t.Errorf("Expected 0 reward for zero difficulty, got %s", reward.String())
		} else {
			t.Logf("✅ Correctly returns 0 for zero difficulty")
		}
	})
}
