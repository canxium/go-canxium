// Copyright 2017 The go-ethereum Authors
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

package canxium

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
)

// mine is the actual proof-of-work miner that searches for a nonce starting from
// seed that results in correct final block difficulty.
func (canxium *Canxium) ethashMine(transaction *types.Transaction, id int, seed uint64, abort chan struct{}, found chan *types.Transaction) {
	// Extract some data from the header
	var (
		hash   = transaction.MiningHash().Bytes()
		target = new(big.Int).Div(two256, transaction.Difficulty())
	)
	// Start generating random nonces until we abort or find a good one
	var (
		attempts  = int64(0)
		nonce     = seed
		powBuffer = new(big.Int)
	)
	logger := canxium.config.Log.New("miner", id)
	logger.Info("Started ethash search for new nonce for transaction mining", "seed", seed)
search:
	for {
		select {
		case <-abort:
			// Mining terminated, update stats and abort
			logger.Info("Ethash nonce search aborted", "attempts", nonce-seed)
			canxium.hashrate.Mark(attempts)
			break search

		default:
			// We don't have to update hash rate on every nonce, so update after after 2^X nonces
			attempts++
			if (attempts % (1 << 15)) == 0 {
				canxium.hashrate.Mark(attempts)
				attempts = 0
			}
			// Compute the PoW value of this nonce
			digest, result := ethash.HashimotoFull(canxium.dataset, hash, nonce)
			if powBuffer.SetBytes(result).Cmp(target) <= 0 {
				canxium.config.Log.Info("Found nonce for mine transaction", "hash", transaction.Hash(), "none", nonce, "digest", common.BytesToHash(digest))
				transaction.SetPow(nonce, common.BytesToHash(digest))
				select {
				case found <- transaction:
					logger.Trace("Ethash nonce found and reported", "attempts", nonce-seed, "nonce", nonce)
				case <-abort:
					logger.Trace("Ethash nonce found but discarded", "attempts", nonce-seed, "nonce", nonce)
				}
				break search
			}
			nonce++
		}
	}
}
