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
	KawPoWAlgorithm
)

type CrossChain uint16

const (
	UnknownChain CrossChain = iota
	KaspaChain
	RavenChain
)

const (
	// prefix of kaspa miner in the coinbase transaction payload. To extract the canxium address
	minerTagPrefix     = "canxiuminer:"
	utxoMinerTagPrefix = "CAU:" // smaller prefix for UTXO miner tag
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
	// Return block number, if any
	BlockNumber() uint64
	// Block hash, in string
	BlockHash() string
	// Return Seal hash, in string, this hash will be used for PoW generation
	SealHash() string
	// Return mix hash, in string, if any
	MixHash() string
	// Return header bits, used to encode the target threshold
	Bits() uint64
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
