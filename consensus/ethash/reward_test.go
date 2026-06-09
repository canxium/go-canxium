// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethash

import (
	"math/big"
	"testing"
)

// TestBlockRewardHalving verifies the per-block reward halves exactly at era
// boundaries (early on, where the cap does not yet bind).
func TestBlockRewardHalving(t *testing.T) {
	era := Pow2HalvingEraBlocks
	cases := []struct {
		num  uint64
		want *big.Int
	}{
		{1, new(big.Int).Set(Pow2InitialReward)},            // era 0
		{era - 1, new(big.Int).Set(Pow2InitialReward)},      // last block of era 0
		{era, new(big.Int).Rsh(Pow2InitialReward, 1)},       // first block of era 1
		{2*era - 1, new(big.Int).Rsh(Pow2InitialReward, 1)}, // last block of era 1
		{2 * era, new(big.Int).Rsh(Pow2InitialReward, 2)},   // first block of era 2
		{5 * era, new(big.Int).Rsh(Pow2InitialReward, 5)},   // era 5
	}
	for _, c := range cases {
		if got := blockReward(new(big.Int).SetUint64(c.num)); got.Cmp(c.want) != 0 {
			t.Errorf("blockReward(%d) = %s, want %s", c.num, got, c.want)
		}
	}
}

// TestBlockRewardDustEra checks the reward integer-shifts to zero once the era
// is large enough, ending emission.
func TestBlockRewardDustEra(t *testing.T) {
	// The era at which R0 >> era == 0.
	dustEra := uint64(Pow2InitialReward.BitLen()) // 2^BitLen > R0 >= 2^(BitLen-1)
	num := new(big.Int).SetUint64(dustEra * Pow2HalvingEraBlocks)
	if got := blockReward(num); got.Sign() != 0 {
		t.Errorf("blockReward at dust era %d = %s, want 0", dustEra, got)
	}
}

// TestEmissionCap verifies cumulative emission never exceeds the cap, gets very
// close to it, and that minting any single block keeps the running total within
// the cap.
func TestEmissionCap(t *testing.T) {
	// Far past the dust era: all reward has been emitted.
	end := 100 * Pow2HalvingEraBlocks
	total := cumulativeEmission(end)

	if total.Cmp(Pow2MiningCap) > 0 {
		t.Fatalf("cumulative emission %s exceeds cap %s", total, Pow2MiningCap)
	}
	// Should reach at least 99.9% of the cap (undershoot is only integer
	// truncation dust, far below 0.1% of 28.5M).
	floor := new(big.Int).Div(new(big.Int).Mul(Pow2MiningCap, big.NewInt(999)), big.NewInt(1000))
	if total.Cmp(floor) < 0 {
		t.Errorf("cumulative emission %s below 99.9%% of cap %s", total, Pow2MiningCap)
	}

	// minted-before(n) + reward(n) must never exceed the cap.
	for _, era := range []uint64{0, 1, 2, 10, 57, 58, 60} {
		n := era * Pow2HalvingEraBlocks
		minted := cumulativeEmission(n)
		withThis := new(big.Int).Add(minted, blockReward(new(big.Int).SetUint64(n)))
		if withThis.Cmp(Pow2MiningCap) > 0 {
			t.Errorf("era %d: minted+reward %s exceeds cap %s", era, withThis, Pow2MiningCap)
		}
	}
}

// TestCumulativeEmissionBruteForce cross-checks the closed-form cumulative sum
// against a direct per-block summation, using shrunk constants so the brute
// force is cheap. This exercises the genesis (block 0) exclusion and the
// full-era / partial-era boundary handling.
func TestCumulativeEmissionBruteForce(t *testing.T) {
	origEra, origReward := Pow2HalvingEraBlocks, Pow2InitialReward
	defer func() { Pow2HalvingEraBlocks, Pow2InitialReward = origEra, origReward }()

	Pow2HalvingEraBlocks = 10
	Pow2InitialReward = big.NewInt(1 << 12) // 4096, halves cleanly down to 0

	brute := func(n uint64) *big.Int {
		sum := new(big.Int)
		for i := uint64(1); i < n; i++ { // block 0 is genesis, never minted
			era := i / Pow2HalvingEraBlocks
			sum.Add(sum, new(big.Int).Rsh(Pow2InitialReward, uint(era)))
		}
		return sum
	}
	for n := uint64(0); n <= 205; n++ {
		want := brute(n)
		if got := cumulativeEmission(n); got.Cmp(want) != 0 {
			t.Fatalf("cumulativeEmission(%d) = %s, brute force = %s", n, got, want)
		}
	}
}
