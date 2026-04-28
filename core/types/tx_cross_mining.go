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
	crosschain "github.com/ethereum/go-ethereum/core/types/cross-chain"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	ErrMergeTxChainNotSupported = errors.New("merge transaction chain not supported")
)

type CrossMiningTx struct {
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
	AuxPoW crosschain.CrossChainBlock

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

type RlpCrossMiningTx struct {
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
	AuxPoW []byte

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`
}

// copy creates a deep copy of the transaction data and initializes all decoded.
func (tx *CrossMiningTx) copy() TxData {
	auxPoW := tx.AuxPoW.Copy()
	cpy := &CrossMiningTx{
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
		// cross mining fields
		AuxPoW: auxPoW,
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
func (tx *CrossMiningTx) txType() byte           { return CrossMiningTxType }
func (tx *CrossMiningTx) chainID() *big.Int      { return tx.ChainID }
func (tx *CrossMiningTx) accessList() AccessList { return nil }
func (tx *CrossMiningTx) data() []byte           { return tx.Data }
func (tx *CrossMiningTx) gas() uint64            { return tx.Gas }
func (tx *CrossMiningTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *CrossMiningTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *CrossMiningTx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *CrossMiningTx) value() *big.Int        { return tx.Value }
func (tx *CrossMiningTx) nonce() uint64          { return tx.Nonce }
func (tx *CrossMiningTx) to() *common.Address    { return &tx.To }
func (tx *CrossMiningTx) from() common.Address   { return tx.From }

func (tx *CrossMiningTx) auxPoW() crosschain.CrossChainBlock { return tx.AuxPoW }
func (tx *CrossMiningTx) algorithm() crosschain.PoWAlgorithm {
	if tx.AuxPoW == nil {
		return crosschain.NoneAlgorithm
	}
	return tx.AuxPoW.PoWAlgorithm()
}
func (tx *CrossMiningTx) difficulty() *big.Int {
	if tx.AuxPoW == nil {
		return common.Big0
	}
	return tx.AuxPoW.Difficulty()
}
func (tx *CrossMiningTx) powNonce() uint64       { return 0 }
func (tx *CrossMiningTx) mixDigest() common.Hash { return common.Hash{} }

func (tx *CrossMiningTx) effectiveGasPrice(dst *big.Int, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return dst.Set(tx.GasFeeCap)
	}
	tip := dst.Sub(tx.GasFeeCap, baseFee)
	if tip.Cmp(tx.GasTipCap) > 0 {
		tip.Set(tx.GasTipCap)
	}
	return tip.Add(tip, baseFee)
}

func (tx *CrossMiningTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *CrossMiningTx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.ChainID, tx.V, tx.R, tx.S = chainID, v, r, s
}

type WrapData struct {
	TypeID byte
	Data   crosschain.CrossChainBlock
}

func EncodeCrossChainBlock(mb crosschain.CrossChainBlock) ([]byte, error) {
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

func DecodeCrossChainBlock(data []byte) (crosschain.CrossChainBlock, error) {
	if len(data) == 0 {
		return nil, errShortTypedTx // No merge block present
	}

	switch crosschain.CrossChain(data[0]) {
	case crosschain.KaspaChain:
		var proof crosschain.KaspaBlock
		err := rlp.DecodeBytes(data[1:], &proof)
		return &proof, err
	default:
		return nil, ErrMergeTxChainNotSupported
	}
}

func (tx *CrossMiningTx) EncodeRLP(w io.Writer) error {
	// Encode all fields, including CrossChainBlock
	crossBlockBytes, err := EncodeCrossChainBlock(tx.AuxPoW)
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
		crossBlockBytes, // Serialized CrossChainBlock as bytes
		// Signature values
		tx.V,
		tx.R,
		tx.S,
	})
}

func (tx *CrossMiningTx) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpCrossMiningTx
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
	tx.V = decoded.V
	tx.R = decoded.R
	tx.S = decoded.S

	if len(decoded.AuxPoW) > 0 {
		crossBlock, err := DecodeCrossChainBlock(decoded.AuxPoW)
		if err != nil {
			return err
		}

		tx.AuxPoW = crossBlock
	}

	return nil
}
