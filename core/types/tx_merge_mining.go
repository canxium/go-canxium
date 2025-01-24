// Copyright 2021 The go-ethereum Authors
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

package types

import (
	"bytes"
	"errors"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type MergeChain uint16

const (
	UnknownChain MergeChain = iota
	KaspaChain
)

var (
	ErrMergeTxChainNotSupported = errors.New("merge transaction chain not supported")
)

type MergeBlock interface {
	Chain() MergeChain
	// Basic check if this is a valid merge mining block
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
}

type MergeMiningTx struct {
	ChainID   *big.Int
	Nonce     uint64   // sender nonce
	GasTipCap *big.Int // a.k.a. maxPriorityFeePerGas
	GasFeeCap *big.Int // a.k.a. maxFeePerGas
	Gas       uint64
	From      common.Address // sender address, to prevent replay attack
	To        common.Address // mining reward receiver
	Value     *big.Int       // value should equal difficulty * consensus reward per difficulty hash
	Data      []byte

	// Merge mining fields
	Algorithm  uint8 // hash algorithm: sha-256, scrypt...
	MergeProof MergeBlock

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

type RlpMergeMiningTx struct {
	ChainID   *big.Int
	Nonce     uint64   // sender nonce
	GasTipCap *big.Int // a.k.a. maxPriorityFeePerGas
	GasFeeCap *big.Int // a.k.a. maxFeePerGas
	Gas       uint64
	From      common.Address // sender address, to prevent replay attack
	To        common.Address // mining reward receiver
	Value     *big.Int       // value should equal difficulty * consensus reward per difficulty hash
	Data      []byte

	// Merge mining fields
	Algorithm  uint8 // hash algorithm: sha-256, scrypt...
	MergeProof []byte

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

// copy creates a deep copy of the transaction data and initializes all decoded.
func (tx *MergeMiningTx) copy() TxData {
	cpy := &MergeMiningTx{
		Nonce: tx.Nonce,
		From:  tx.From,
		To:    tx.To,
		Data:  common.CopyBytes(tx.Data),
		Gas:   tx.Gas,
		// These are copied below.
		Value:     new(big.Int),
		ChainID:   new(big.Int),
		GasTipCap: new(big.Int),
		GasFeeCap: new(big.Int),
		// merge mining fields
		Algorithm:  tx.Algorithm,
		MergeProof: tx.MergeProof,
		// signature
		V: new(big.Int),
		R: new(big.Int),
		S: new(big.Int),
	}

	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}
	if tx.ChainID != nil {
		cpy.ChainID.Set(tx.ChainID)
	}
	if tx.GasTipCap != nil {
		cpy.GasTipCap.Set(tx.GasTipCap)
	}
	if tx.GasFeeCap != nil {
		cpy.GasFeeCap.Set(tx.GasFeeCap)
	}

	if tx.V != nil {
		cpy.V.Set(tx.V)
	}
	if tx.R != nil {
		cpy.R.Set(tx.R)
	}
	if tx.S != nil {
		cpy.S.Set(tx.S)
	}
	return cpy
}

// accessors for innerTx.
func (tx *MergeMiningTx) txType() byte           { return MergeMiningTxType }
func (tx *MergeMiningTx) chainID() *big.Int      { return tx.ChainID }
func (tx *MergeMiningTx) accessList() AccessList { return nil }
func (tx *MergeMiningTx) data() []byte           { return tx.Data }
func (tx *MergeMiningTx) gas() uint64            { return tx.Gas }
func (tx *MergeMiningTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *MergeMiningTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *MergeMiningTx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *MergeMiningTx) value() *big.Int        { return tx.Value }
func (tx *MergeMiningTx) nonce() uint64          { return tx.Nonce }
func (tx *MergeMiningTx) to() *common.Address    { return &tx.To }
func (tx *MergeMiningTx) from() common.Address   { return tx.From }

func (tx *MergeMiningTx) mergeProof() MergeBlock { return tx.MergeProof }
func (tx *MergeMiningTx) algorithm() byte        { return tx.Algorithm }
func (tx *MergeMiningTx) difficulty() *big.Int {
	if tx.MergeProof == nil {
		return common.Big0
	}
	return tx.MergeProof.Difficulty()
}
func (tx *MergeMiningTx) powNonce() uint64       { return 0 }
func (tx *MergeMiningTx) mixDigest() common.Hash { return common.Hash{} }

func (tx *MergeMiningTx) effectiveGasPrice(dst *big.Int, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return dst.Set(tx.GasFeeCap)
	}
	tip := dst.Sub(tx.GasFeeCap, baseFee)
	if tip.Cmp(tx.GasTipCap) > 0 {
		tip.Set(tx.GasTipCap)
	}
	return tip.Add(tip, baseFee)
}

func (tx *MergeMiningTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *MergeMiningTx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.ChainID, tx.V, tx.R, tx.S = chainID, v, r, s
}

type WrapData struct {
	TypeID byte
	Data   MergeBlock
}

func EncodeMergeBlock(mb MergeBlock) ([]byte, error) {
	if mb == nil {
		return nil, nil
	}

	buf := new(bytes.Buffer)
	buf.WriteByte(byte(mb.Chain()))
	if err := rlp.Encode(buf, mb); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeMergeBlock(data []byte) (MergeBlock, error) {
	if len(data) == 0 {
		return nil, errShortTypedTx // No merge block present
	}

	switch MergeChain(data[0]) {
	case KaspaChain:
		var proof KaspaBlock
		err := rlp.DecodeBytes(data[1:], &proof)
		return &proof, err
	default:
		return nil, ErrMergeTxChainNotSupported
	}
}

func (tx *MergeMiningTx) EncodeRLP(w io.Writer) error {
	// Encode all fields, including MergeBlock
	mergeBlockBytes, err := EncodeMergeBlock(tx.MergeProof)
	if err != nil {
		return err
	}

	return rlp.Encode(w, []interface{}{
		tx.ChainID,
		tx.Nonce,
		tx.GasTipCap,
		tx.GasFeeCap,
		tx.Gas,
		tx.From,
		tx.To,
		tx.Value,
		tx.Data,
		tx.Algorithm,
		mergeBlockBytes, // Serialized MergeBlock as bytes
		// Signature values
		tx.V,
		tx.R,
		tx.S,
	})
}

func (tx *MergeMiningTx) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpMergeMiningTx
	if err := s.Decode(&decoded); err != nil {
		return err
	}

	tx.ChainID = decoded.ChainID
	tx.Nonce = decoded.Nonce
	tx.GasTipCap = decoded.GasTipCap
	tx.GasFeeCap = decoded.GasFeeCap
	tx.Gas = decoded.Gas
	tx.From = decoded.From
	tx.To = decoded.To
	tx.Value = decoded.Value
	tx.Data = decoded.Data
	tx.Algorithm = decoded.Algorithm
	tx.V = decoded.V
	tx.R = decoded.R
	tx.S = decoded.S

	if len(decoded.MergeProof) > 0 {
		mergeBlock, err := DecodeMergeBlock(decoded.MergeProof)
		if err != nil {
			return err
		}

		tx.MergeProof = mergeBlock
	}

	return nil
}
