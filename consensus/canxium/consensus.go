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

// Package clique implements the proof-of-authority consensus engine.
package canxium

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"golang.org/x/crypto/sha3"
)

var (
	errInvalidDifficulty = errors.New("non-positive difficulty")
	errInvalidMixDigest  = errors.New("invalid mix digest")
	errInvalidPoW        = errors.New("invalid proof-of-work")
	errInvalidTxType     = errors.New("invalid offline mining transaction type")
)

// SignerFn hashes and signs the data to be signed by a backing account.
type SignerFn func(signer accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)

// Author implements consensus.Engine, returning the verified author of the block.
func (c *Canxium) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// This consensus won't process block, no header verifier
func (c *Canxium) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	return fmt.Errorf("Canxium offline transaction only")
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers. The
// method returns a quit channel to abort the operations and a results channel to
// retrieve the async verifications (the order is that of the input slice).
func (c *Canxium) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for i, header := range headers {
			err := c.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (c *Canxium) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	return fmt.Errorf("Canxium offline transaction mining only")
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (c *Canxium) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return fmt.Errorf("no uncles is verified in canxium offline transaction consensus")
}

// verifySeal checks whether a block satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (c *Canxium) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, fulldag bool) error {
	return nil
}

// verifyTxSeal checks whether a offline mining transaction satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (c *Canxium) VerifyTxSeal(transaction *types.Transaction, fulldag bool) error {
	switch transaction.Algorithm() {
	case types.EthashAlgorithm:
		return c.ethash.VerifyTxSeal(transaction, fulldag)
	default:
		return fmt.Errorf("offline mining algorithm %d is not supported yet", transaction.Algorithm())
	}
}

// verifyTxsSeal checks whether offline mining transactions satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (c *Canxium) VerifyTxsSeal(transactions types.Transactions, fulldag bool) <-chan error {
	return c.ethash.VerifyTxsSeal(transactions, fulldag)
}

// Prepare implements consensus.Engine, preparing all the consensus fields of the
// header for running the transactions on top.
func (c *Canxium) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	// all headers information is fake in canxium tx mining consensus
	header.Coinbase = common.Address{}
	header.Nonce = types.BlockNonce{}

	// Set the correct difficulty
	header.Difficulty = c.config.Difficulty

	// Mix digest is reserved for now, set to empty
	header.MixDigest = common.Hash{}

	header.Time = uint64(time.Now().Unix())
	return nil
}

// Finalize implements consensus.Engine. There is no post-transaction
// consensus rules in clique, do nothing here.
func (c *Canxium) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal) {
	// Do nothing
}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (c *Canxium) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt, withdrawals []*types.Withdrawal) (*types.Block, error) {
	if len(withdrawals) > 0 {
		return nil, errors.New("canxium does not support withdrawals")
	}

	if len(txs) != 1 {
		return nil, errors.New("invalid block transactions for finalize")
	}

	// Finalize block
	c.Finalize(chain, header, state, txs, uncles, nil)

	// Assign the final state root to header.
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Assemble and return the final block for sealing.
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil)), nil
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (c *Canxium) Authorize(signer common.Address, signFn SignerFn) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.signer = signer
	c.signFn = signFn
}

func (c *Canxium) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return c.config.Difficulty
}

// SealHash returns the hash of a block prior to it being sealed.
func (c *Canxium) SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()

	enc := []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra,
	}
	if header.BaseFee != nil {
		enc = append(enc, header.BaseFee)
	}
	if header.MinerReward != nil {
		enc = append(enc, header.MinerReward)
	}
	if header.FundReward != nil {
		enc = append(enc, header.FundReward)
	}
	if header.WithdrawalsHash != nil {
		panic("withdrawal hash set on ethash")
	}
	rlp.Encode(hasher, enc)
	hasher.Sum(hash[:0])
	return hash
}
