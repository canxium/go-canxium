package misc

import (
	"math/big"
	"testing"
)

func TestKaspaMergeMiningReward(t *testing.T) {
	// Test parameters
	difficulty := big.NewInt(1000000000000000000) // Example difficulty value

	// Calculate reward
	rewardDay0, _ := kaspaMergeMiningReward(difficulty, 0)
	if rewardDay0.Cmp(big.NewInt(500000000000000000)) != 0 {
		t.Errorf("Day 0: Reward %s should equal %d", rewardDay0.String(), 500000000000000000)
	}

	rewardDay2, _ := kaspaMergeMiningReward(difficulty, 2)
	if rewardDay2.Cmp(big.NewInt(367821127229820687)) != 0 {
		t.Errorf("Day 2: Reward %s should equal %d", rewardDay2.String(), 367821127229820687)
	}

	rewardDay33, _ := kaspaMergeMiningReward(difficulty, 33)
	if rewardDay33.Cmp(big.NewInt(125959453857468844)) != 0 {
		t.Errorf("Day 33: Reward %s should equal %d", rewardDay33.String(), 125959453857468844)
	}

	rewardDay1735, _ := kaspaMergeMiningReward(difficulty, 1735)
	if rewardDay1735.Cmp(big.NewInt(974196077977997)) != 0 {
		t.Errorf("Day 1734: Reward %s should equal %d", rewardDay1735.String(), 974196077977997)
	}
}

func TestDayNumberBetweenTime(t *testing.T) {
	// Test cases for dayNumberBetweenTime
	tests := []struct {
		forkTime uint64
		time     uint64
		expected uint64
	}{
		{1700000000, 1700000000, 0}, // Same time
		{1700000000, 1700086400, 1}, // 1 day later
		{1700000000, 1700172800, 2}, // 2 days later
		{1700000000, 1699913600, 0}, // Before forkTime
	}

	for _, test := range tests {
		dayNum := dayNumberBetweenTime(test.forkTime, test.time)
		if dayNum != test.expected {
			t.Errorf("For forkTime %d and time %d, expected %d but got %d",
				test.forkTime, test.time, test.expected, dayNum)
		}
	}
}
