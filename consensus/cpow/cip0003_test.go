// Copyright 2026 The go-canxium Authors
// This file is part of the go-canxium library.
//
// The go-canxium library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cpow

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// fakeState is a minimal WDCStateReader backed by a slot map, used to stage WDC
// contract storage for tests without a real StateDB.
type fakeState map[common.Hash]common.Hash

func (f fakeState) GetState(addr common.Address, key common.Hash) common.Hash {
	if addr != WdcAddress {
		return common.Hash{}
	}
	return f[key]
}

type minerSpec struct {
	addr   common.Address
	start  uint64
	end    uint64
	signer common.Address
}

// buildState writes the WDC storage slots for the given miners, mirroring the
// exact layout that LoadMiners / readMinerData read (miners[] array at slot 2,
// minerNonces mapping at slot 3, packed start|end<<64 at struct offset 2,
// signer at offset 5).
func buildState(miners []minerSpec) fakeState {
	s := fakeState{}
	arrayLenHash := common.BigToHash(big.NewInt(WDCMinersArraySlot))
	s[arrayLenHash] = common.BigToHash(big.NewInt(int64(len(miners))))
	base := crypto.Keccak256Hash(arrayLenHash[:]).Big()

	for i, m := range miners {
		// miners[i] = address
		elemSlot := common.BigToHash(new(big.Int).Add(base, big.NewInt(int64(i))))
		s[elemSlot] = common.BytesToHash(m.addr.Bytes())

		// minerNonces[addr] base slot
		buf := make([]byte, 64)
		copy(buf[12:32], m.addr[:])
		buf[63] = byte(WDCMapSlot)
		structBase := crypto.Keccak256Hash(buf).Big()

		// offset 2: packed start (bits 0-63) | end (bits 64-127)
		packed := new(big.Int).SetUint64(m.end)
		packed.Lsh(packed, 64)
		packed.Or(packed, new(big.Int).SetUint64(m.start))
		s[common.BigToHash(new(big.Int).Add(structBase, big.NewInt(2)))] = common.BigToHash(packed)

		// offset 5: signer
		s[common.BigToHash(new(big.Int).Add(structBase, big.NewInt(5)))] = common.BytesToHash(m.signer.Bytes())
	}
	return s
}

var (
	addrA = common.HexToAddress("0x00000000000000000000000000000000000000aa")
	addrB = common.HexToAddress("0x00000000000000000000000000000000000000bb")
	addrC = common.HexToAddress("0x00000000000000000000000000000000000000cc")
	sigA  = common.HexToAddress("0x1111111111111111111111111111111111111111")
	sigB  = common.HexToAddress("0x2222222222222222222222222222222222222222")
	sigC  = common.HexToAddress("0x3333333333333333333333333333333333333333")
)

func TestLoadMinersAndLookup(t *testing.T) {
	// Deliberately out of Start order to exercise the sort; disjoint ranges.
	st := buildState([]minerSpec{
		{addrB, 1000, 1999, sigB},
		{addrA, 0, 999, sigA},
		{addrC, 2000, 2999, sigC},
	})
	ms := LoadMiners(st)

	// ByNonce hits the right miner across range boundaries.
	cases := []struct {
		nonce uint64
		want  common.Address
	}{
		{0, addrA}, {999, addrA}, {1000, addrB}, {1999, addrB}, {2000, addrC}, {2999, addrC},
	}
	for _, c := range cases {
		got := ms.ByNonce(c.nonce)
		if got == nil || got.Miner != c.want {
			t.Errorf("ByNonce(%d) = %v, want %s", c.nonce, got, c.want.Hex())
		}
	}
	if ms.ByNonce(3000) != nil {
		t.Errorf("ByNonce(3000) should be nil (outside all ranges)")
	}

	// ByAddress returns full record including signer and array index.
	if m := ms.ByAddress(addrB); m == nil || m.Signer != sigB || m.Start != 1000 || m.End != 1999 || m.Index != 0 {
		t.Errorf("ByAddress(addrB) = %+v, want start=1000 end=1999 signer=%s index=0", m, sigB.Hex())
	}
	if ms.ByAddress(common.HexToAddress("0xdead")) != nil {
		t.Errorf("ByAddress(unknown) should be nil")
	}
}

func TestLoadMinersEmpty(t *testing.T) {
	ms := LoadMiners(fakeState{})
	if ms == nil {
		t.Fatal("LoadMiners returned nil for empty state")
	}
	if ms.ByNonce(0) != nil || ms.ByAddress(addrA) != nil {
		t.Errorf("empty miner set should return nil for all lookups")
	}
}

// TestForkSafety is the regression test for the redesign: two states at the same
// logical epoch but with different miner ranges must produce different results,
// and the root-keyed cache must never serve one state's ranges for the other.
func TestForkSafety(t *testing.T) {
	forkX := buildState([]minerSpec{{addrA, 0, 4999, sigA}}) // A owns 0..4999
	forkY := buildState([]minerSpec{{addrB, 0, 4999, sigB}}) // B owns the same range
	rootX := common.HexToHash("0x01")
	rootY := common.HexToHash("0x02")

	cache := NewWDCCache()
	msX := cache.Miners(rootX, forkX)
	msY := cache.Miners(rootY, forkY)

	if m := msX.ByNonce(100); m == nil || m.Miner != addrA {
		t.Errorf("fork X nonce 100 should belong to addrA, got %v", m)
	}
	if m := msY.ByNonce(100); m == nil || m.Miner != addrB {
		t.Errorf("fork Y nonce 100 should belong to addrB, got %v", m)
	}
	if msX == msY {
		t.Errorf("distinct roots must yield distinct miner sets")
	}

	// Same root ⇒ cache hit ⇒ identical pointer (no re-derivation).
	if cache.Miners(rootX, forkX) != msX {
		t.Errorf("same root should return the cached miner set")
	}
}
