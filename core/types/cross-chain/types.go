// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package crosschain

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type PoWAlgorithm uint8

// Transaction mining algorithm.
const (
	NoneAlgorithm PoWAlgorithm = iota
	EthashAlgorithm
	Sha256Algorithm
	ScryptAlgorithm
	KHeavyHashAlgorithm
	RandomXAlgorithm
)

type CrossChain uint16

const (
	UnknownChain CrossChain = iota
	KaspaChain
	MoneroChain
)

const (
	// prefix of kaspa miner in the coinbase transaction payload. To extract the canxium address
	minerTagPrefix = "canxiuminer:"
)

var (
	bigOne = big.NewInt(1)
	// mainPowMax is the highest proof of work value a Kaspa block can
	// have for the main network. It is the value 2^255 - 1.
	mainPowMax  = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)
	zeroAddress = common.Address{}
)

var (
	ErrInvalidCrossChainBlockHeader = errors.New("invalid cross mining block header")
)

type CrossChainBlock interface {
	Chain() CrossChain
	// Basic check if this is a valid cross mining block
	IsValidBlock() bool
	// Verify block PoW
	VerifyPoW() error
	// Verify coinbase transaction if follow consensus rules
	VerifyCoinbase() bool
	// Canxium miner address
	GetMinerAddress() (common.Address, error)
	// Block hash, in string
	BlockHash() string
	// Block difficulty
	Difficulty() *big.Int
	// Nonce number of the block
	PowNonce() uint64
	// block timestamp in millisecond
	Timestamp() uint64
	// PoW Algorithm
	PoWAlgorithm() PoWAlgorithm
	// Deep copy
	Copy() CrossChainBlock
}

const (
	// Monero constants
	RandomXActivationTimestamp = 1561234567 // Example timestamp, replace with actual Monero RandomX activation timestamp
)
