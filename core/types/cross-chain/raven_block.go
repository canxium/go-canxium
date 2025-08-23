// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package crosschain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// RavenBlockHeader defines information about a Raven coin block
type RavenBlockHeader struct {
	// Version of the block
	Version uint32 `json:"version"`

	// PrevBlock is the hash of the previous block in the chain
	PrevBlock common.Hash `json:"prevBlock"`

	// MerkleRoot is the merkle tree reference to hash of all transactions for the block
	MerkleRoot common.Hash `json:"merkleRoot"`

	// Time the block was created (Unix timestamp)
	Timestamp uint32 `json:"timestamp"`

	// Bits represents the difficulty target for the block
	Bits uint32 `json:"bits"`

	// Height of the block in the chain
	Height uint32 `json:"height"`

	// Nonce is the full 64-bit nonce used in KawPoW
	Nonce uint64 `json:"nonce,omitempty"`

	MixHash common.Hash `json:"mixHash,omitempty"` // Mix hash for KawPoW

	// cache header hash
	hash common.Hash `json:"-"` // Cached header hash for performance
}

type RlpRavenBlockHeader struct {
	Version    uint32
	PrevBlock  []byte
	MerkleRoot []byte
	Timestamp  uint32
	Bits       uint32
	Nonce      uint64
	Height     uint32
}

func (header *RavenBlockHeader) clone() *RavenBlockHeader {
	return &RavenBlockHeader{
		Version:    header.Version,
		PrevBlock:  header.PrevBlock,
		MerkleRoot: header.MerkleRoot,
		Timestamp:  header.Timestamp,
		Bits:       header.Bits,
		Nonce:      header.Nonce,
		Height:     header.Height,
	}
}

// HeaderHash computes the header hash for KawPoW validation
// This implements the exact CKAWPOWInput serialization from Ravencoin's C++ code
func (header *RavenBlockHeader) HeaderHash() common.Hash {
	if header.hash != (common.Hash{}) {
		return header.hash
	}

	// Create a buffer for CKAWPOWInput serialization
	buf := bytes.NewBuffer(nil)

	// READWRITE(nVersion);
	binary.Write(buf, binary.LittleEndian, header.Version)

	// READWRITE(hashPrevBlock);
	// In Bitcoin/Ravencoin, hashes are stored in reverse byte order (little-endian)
	// when serialized compared to their hex representation
	prevBlockReversed := make([]byte, 32)
	for i := 0; i < 32; i++ {
		prevBlockReversed[i] = header.PrevBlock[31-i]
	}
	buf.Write(prevBlockReversed)

	// READWRITE(hashMerkleRoot);
	// Same reverse byte order for merkle root
	merkleRootReversed := make([]byte, 32)
	for i := 0; i < 32; i++ {
		merkleRootReversed[i] = header.MerkleRoot[31-i]
	}
	buf.Write(merkleRootReversed)

	// READWRITE(nTime);
	binary.Write(buf, binary.LittleEndian, header.Timestamp)

	// READWRITE(nBits);
	binary.Write(buf, binary.LittleEndian, header.Bits)

	// READWRITE(nHeight);
	binary.Write(buf, binary.LittleEndian, header.Height)

	// Note: CKAWPOWInput does NOT include nNonce64 or mix_hash in serialization
	// Those are used by the KawPoW algorithm but not part of the header hash

	// Return the double SHA256 hash (same as SerializeHash in C++)
	first := sha256.Sum256(buf.Bytes())
	second := sha256.Sum256(first[:])

	// The resulting hash should also be reversed for display consistency with Ravencoin
	result := make([]byte, 32)
	for i := 0; i < 32; i++ {
		result[i] = second[31-i]
	}

	header.hash = common.BytesToHash(result)
	return header.hash
}

// VerifyKawPoW verifies the KawPoW proof of work for this header
// This implements the exact same logic as Ravencoin's KAWPOWHash() function
func (header *RavenBlockHeader) VerifyKawPoW() error {
	// Generate the KawPoW hash and mix hash
	finalHash, calculatedMixHash, err := header.GenerateKawPoW()
	if err != nil {
		return err
	}

	// Verify the mix hash matches what's stored in the header
	if calculatedMixHash != header.MixHash {
		return fmt.Errorf("mix hash mismatch: expected %s, got %s", header.MixHash.Hex(), calculatedMixHash.Hex())
	}

	// Check if the final hash meets the difficulty target
	target := bitsToTarget(header.Bits)
	finalHashBig := finalHash.Big()

	// In PoW, the hash must be <= target to be valid
	if finalHashBig.Cmp(target) <= 0 {
		return nil // Valid KawPoW proof of work
	}

	return fmt.Errorf("final hash %s exceeds target %s", finalHash.Hex(), target.Text(16))
}

// GenerateKawPoW generates a KawPoW hash and mix hash for this header
// This implements the same logic as Ravencoin's KAWPOWHash() function
func (header *RavenBlockHeader) GenerateKawPoW() (common.Hash, common.Hash, error) {
	// Get the header hash (same as blockHeader.GetKAWPOWHeaderHash() in C++)
	headerHash := header.HeaderHash()

	// Convert to the format expected by kawpow-cgo
	// Remove the "0x" prefix from the hex string
	headerHashStr := headerHash.Hex()[2:]
	nonceStr := fmt.Sprintf("%016x", header.Nonce)
	height := int64(header.Height)

	// Call kawpow hash function (equivalent to progpow::hash() in C++)
	// This matches Ravencoin's KAWPOWHash() implementation
	hash, mixHash := crypto.KawpowHash(headerHashStr, nonceStr, height)

	// Convert the returned byte arrays to common.Hash
	finalHash := common.BytesToHash(hash)
	calculatedMixHash := common.BytesToHash(mixHash)

	return finalHash, calculatedMixHash, nil
}

// GetDifficulty calculates the difficulty from bits field
// This implements the exact same logic as Ravencoin's GetDifficulty() function
func (header *RavenBlockHeader) GetDifficulty() *big.Int {
	// Extract the shift value (exponent) from bits
	nShift := int((header.Bits >> 24) & 0xff)

	// Calculate base difficulty: 0x0000ffff / (bits & 0x00ffffff)
	mantissa := float64(header.Bits & 0x00ffffff)
	dDiff := float64(0x0000ffff) / mantissa

	// Adjust difficulty based on shift value to normalize to shift 29
	for nShift < 29 {
		dDiff *= 256.0
		nShift++
	}
	for nShift > 29 {
		dDiff /= 256.0
		nShift--
	}

	return big.NewInt(int64(dDiff))
}

func (header *RavenBlockHeader) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []interface{}{
		header.Version,
		header.PrevBlock[:],
		header.MerkleRoot[:],
		header.Timestamp,
		header.Bits,
		header.Nonce,
		header.Height,
	})
}

func (header *RavenBlockHeader) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpRavenBlockHeader
	if err := s.Decode(&decoded); err != nil {
		return fmt.Errorf("failed to decode raven block header: %w", err)
	}

	header.Version = decoded.Version
	header.PrevBlock = common.BytesToHash(decoded.PrevBlock)
	header.MerkleRoot = common.BytesToHash(decoded.MerkleRoot)
	header.Timestamp = decoded.Timestamp
	header.Bits = decoded.Bits
	header.Nonce = decoded.Nonce
	header.Height = decoded.Height

	return nil
}

// NewRavenBlockHeader creates a new Raven block header
func NewRavenBlockHeader(
	version uint32,
	prevBlock common.Hash,
	merkleRoot common.Hash,
	timestamp uint32,
	bits uint32,
	nonce uint64,
	height uint32,
) *RavenBlockHeader {
	return &RavenBlockHeader{
		Version:    version,
		PrevBlock:  prevBlock,
		MerkleRoot: merkleRoot,
		Timestamp:  timestamp,
		Bits:       bits,
		Nonce:      nonce,
		Height:     height,
	}
}

type RavenBlock struct {
	Header      *RavenBlockHeader `json:"header"`
	MerkleProof []common.Hash     `json:"merkleProof"` // merkle proof path to verify the coinbase tx
	Coinbase    *RavenTransaction `json:"coinbase"`
	Hash        common.Hash       `json:"hash"` // Block hash
}

type RlpRavenBlock struct {
	Header      *RavenBlockHeader
	MerkleProof []byte
	Coinbase    *RavenTransaction
	Hash        common.Hash
}

// RavenTransaction represents a Ravencoin transaction following their exact format
type RavenTransaction struct {
	Version  int32           `json:"version"` // Changed to int32 to match Ravencoin
	Inputs   []RavenTxInput  `json:"inputs"`
	Outputs  []RavenTxOutput `json:"outputs"`
	LockTime uint32          `json:"lockTime"`
	// cache hash
	hash common.Hash `json:"-"` // Cached hash for performance
}

type RavenTxInput struct {
	PrevTxHash    common.Hash `json:"prevTxHash"`
	PrevIndex     uint32      `json:"prevIndex"`
	ScriptSig     []byte      `json:"scriptSig"`
	Sequence      uint32      `json:"sequence"`
	ScriptWitness [][]byte    `json:"scriptWitness,omitempty"` // Witness data
}

type RavenTxOutput struct {
	Value        int64  `json:"value"` // Changed to int64 to match Ravencoin CAmount
	ScriptPubKey []byte `json:"scriptPubKey"`
}

// RLP encoding/decoding structures for RavenTransaction
type RlpRavenTransaction struct {
	Version  int32
	Inputs   []RlpRavenTxInput
	Outputs  []RlpRavenTxOutput
	LockTime uint32
}

type RlpRavenTxInput struct {
	PrevTxHash    []byte
	PrevIndex     uint32
	ScriptSig     []byte
	Sequence      uint32
	ScriptWitness [][]byte
}

type RlpRavenTxOutput struct {
	Value        int64
	ScriptPubKey []byte
}

func (tx *RavenTransaction) Hash() common.Hash {
	if tx.hash != (common.Hash{}) {
		return tx.hash
	}

	// Implement Ravencoin's exact transaction serialization format
	// Based on SerializeTransaction in Ravencoin/src/primitives/transaction.h
	buf := new(bytes.Buffer)

	// Write nVersion (int32_t) - little endian
	binary.Write(buf, binary.LittleEndian, int32(tx.Version))

	// Write vin count as compact size (varint)
	writeCompactSize(buf, uint64(len(tx.Inputs)))

	// Write all inputs
	for _, input := range tx.Inputs {
		// Write prevout hash (32 bytes)
		buf.Write(input.PrevTxHash[:])
		// Write prevout index (uint32_t) - little endian
		binary.Write(buf, binary.LittleEndian, input.PrevIndex)
		// Write scriptSig with compact size prefix
		writeCompactSize(buf, uint64(len(input.ScriptSig)))
		buf.Write(input.ScriptSig)
		// Write sequence (uint32_t) - little endian
		binary.Write(buf, binary.LittleEndian, input.Sequence)
	}

	// Write vout count as compact size (varint)
	writeCompactSize(buf, uint64(len(tx.Outputs)))

	// Write all outputs
	for _, output := range tx.Outputs {
		// Write value (int64_t) - little endian
		binary.Write(buf, binary.LittleEndian, int64(output.Value))
		// Write scriptPubKey with compact size prefix
		writeCompactSize(buf, uint64(len(output.ScriptPubKey)))
		buf.Write(output.ScriptPubKey)
	}

	// Write nLockTime (uint32_t) - little endian
	binary.Write(buf, binary.LittleEndian, tx.LockTime)

	// Double SHA256 hash (same as CHash256 in Ravencoin)
	first := sha256.Sum256(buf.Bytes())
	second := sha256.Sum256(first[:])

	// In Bitcoin/Ravencoin, transaction IDs are displayed in reverse byte order
	// So we need to reverse the hash bytes to match the expected txid format
	reversedHash := make([]byte, 32)
	for i := 0; i < 32; i++ {
		reversedHash[i] = second[31-i]
	}

	tx.hash = common.BytesToHash(reversedHash)
	return tx.hash
}

func (tx *RavenTransaction) EncodeRLP(w io.Writer) error {
	rlpInputs := make([]RlpRavenTxInput, len(tx.Inputs))
	for i, input := range tx.Inputs {
		rlpInputs[i] = RlpRavenTxInput{
			PrevTxHash:    input.PrevTxHash[:],
			PrevIndex:     input.PrevIndex,
			ScriptSig:     input.ScriptSig,
			Sequence:      input.Sequence,
			ScriptWitness: input.ScriptWitness,
		}
	}

	rlpOutputs := make([]RlpRavenTxOutput, len(tx.Outputs))
	for i, output := range tx.Outputs {
		rlpOutputs[i] = RlpRavenTxOutput{
			Value:        output.Value,
			ScriptPubKey: output.ScriptPubKey,
		}
	}

	return rlp.Encode(w, RlpRavenTransaction{
		Version:  tx.Version,
		Inputs:   rlpInputs,
		Outputs:  rlpOutputs,
		LockTime: tx.LockTime,
	})
}

func (tx *RavenTransaction) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpRavenTransaction
	if err := s.Decode(&decoded); err != nil {
		return fmt.Errorf("failed to decode raven transaction: %w", err)
	}

	tx.Version = decoded.Version
	tx.LockTime = decoded.LockTime

	tx.Inputs = make([]RavenTxInput, len(decoded.Inputs))
	for i, input := range decoded.Inputs {
		tx.Inputs[i] = RavenTxInput{
			PrevTxHash:    common.BytesToHash(input.PrevTxHash),
			PrevIndex:     input.PrevIndex,
			ScriptSig:     input.ScriptSig,
			Sequence:      input.Sequence,
			ScriptWitness: input.ScriptWitness,
		}
	}

	tx.Outputs = make([]RavenTxOutput, len(decoded.Outputs))
	for i, output := range decoded.Outputs {
		tx.Outputs[i] = RavenTxOutput{
			Value:        output.Value,
			ScriptPubKey: output.ScriptPubKey,
		}
	}

	return nil
}

func (b *RavenBlock) Chain() CrossChain {
	return RavenChain
}

func (b *RavenBlock) PoWAlgorithm() PoWAlgorithm {
	return KawPoWAlgorithm
}

// IsValidBlock check to see if this is a valid raven block, header and coinbase are valid
func (b *RavenBlock) IsValidBlock() bool {
	if b.Header == nil {
		return false
	}
	if b.Coinbase == nil {
		return false
	}
	if b.Header.Nonce == 0 || b.Header.Timestamp == 0 || b.Header.Bits == 0 {
		return false
	}
	if len(b.Coinbase.Outputs) == 0 {
		return false
	}
	if len(b.MerkleProof) == 0 {
		return false
	}
	return true
}

func (b *RavenBlock) Copy() CrossChainBlock {
	header := b.Header.clone()

	// Deep copy coinbase transaction
	coinbase := &RavenTransaction{
		Version:  b.Coinbase.Version,
		LockTime: b.Coinbase.LockTime,
	}

	// Copy inputs
	coinbase.Inputs = make([]RavenTxInput, len(b.Coinbase.Inputs))
	for i, input := range b.Coinbase.Inputs {
		// Copy witness data
		var witnessData [][]byte
		if len(input.ScriptWitness) > 0 {
			witnessData = make([][]byte, len(input.ScriptWitness))
			for j, witness := range input.ScriptWitness {
				witnessData[j] = append([]byte(nil), witness...)
			}
		}

		coinbase.Inputs[i] = RavenTxInput{
			PrevTxHash:    input.PrevTxHash,
			PrevIndex:     input.PrevIndex,
			ScriptSig:     append([]byte(nil), input.ScriptSig...),
			Sequence:      input.Sequence,
			ScriptWitness: witnessData,
		}
	}

	// Copy outputs
	coinbase.Outputs = make([]RavenTxOutput, len(b.Coinbase.Outputs))
	for i, output := range b.Coinbase.Outputs {
		coinbase.Outputs[i] = RavenTxOutput{
			Value:        output.Value,
			ScriptPubKey: append([]byte(nil), output.ScriptPubKey...),
		}
	}

	// Copy merkle proof
	merkleProof := make([]common.Hash, len(b.MerkleProof))
	copy(merkleProof, b.MerkleProof)

	block := &RavenBlock{
		Header:      header,
		MerkleProof: merkleProof,
		Coinbase:    coinbase,
	}

	return block
}

func (b *RavenBlock) BlockHash() string {
	return b.Hash.Hex()
}

func (b *RavenBlock) Timestamp() uint64 {
	return uint64(b.Header.Timestamp) * 1000 // Convert to milliseconds
}

// Verify block's PoW
func (b *RavenBlock) VerifyPoW() error {
	return b.Header.VerifyKawPoW()
}

func (b *RavenBlock) Difficulty() *big.Int {
	// Use the header's GetDifficulty method which implements Ravencoin's logic
	return b.Header.GetDifficulty()
}

func (b *RavenBlock) PowNonce() uint64 {
	return uint64(b.Header.Nonce)
}

// VerifyCoinbase verify raven block coinbase transaction
func (b *RavenBlock) VerifyCoinbase() bool {
	if b.Coinbase == nil {
		return false
	}

	// Check if it's a coinbase transaction (first input has null hash and index 0xFFFFFFFF)
	if len(b.Coinbase.Inputs) == 0 {
		return false
	}

	firstInput := b.Coinbase.Inputs[0]
	nullHash := common.Hash{}
	if firstInput.PrevTxHash != nullHash || firstInput.PrevIndex != 0xFFFFFFFF {
		return false
	}

	// Verify merkle proof
	return b.verifyMerkleProofForCoinbaseTx()
}

// GetMinerAddress returns canxium miner address from raven block coinbase
func (b *RavenBlock) GetMinerAddress() (common.Address, error) {
	// 1. Try to extract from scriptSig (coinbase input) - fastest method
	scriptSig := b.Coinbase.Inputs[0].ScriptSig
	if address := extractAddressFromData(scriptSig); address != "" {
		return common.HexToAddress("0x" + address), nil
	}

	// 2. Try to extract from OP_RETURN outputs - fallback method
	for _, output := range b.Coinbase.Outputs {
		// OP_RETURN scripts start with 0x6a and have zero value
		if output.Value == 0 && len(output.ScriptPubKey) > 0 && output.ScriptPubKey[0] == 0x6a {
			if address := extractAddressFromData(output.ScriptPubKey); address != "" {
				return common.HexToAddress("0x" + address), nil
			}
		}
	}

	return zeroAddress, errors.New("canxium address not found in coinbase scriptSig or OP_RETURN output")
}

// extractAddressFromData safely extracts a 40-character hex address after "CAU:" prefix
func extractAddressFromData(data []byte) string {
	if len(data) < 44 { // minimum: "CAU:" (4) + address (40) = 44 bytes
		return ""
	}

	dataStr := string(data)
	cauIndex := strings.Index(dataStr, utxoMinerTagPrefix) // "CAU:"
	if cauIndex == -1 {
		return ""
	}

	// Start after "CAU:"
	startIndex := cauIndex + len(utxoMinerTagPrefix)

	// Check if there's a "0x" prefix and skip it
	if startIndex+2 < len(dataStr) && dataStr[startIndex:startIndex+2] == "0x" {
		startIndex += 2
	}

	// Extract exactly 40 characters for the address
	if startIndex+40 > len(dataStr) {
		return "" // Not enough data for full address
	}

	address := dataStr[startIndex : startIndex+40]

	// Validate address contains only hex characters
	for _, c := range address {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "" // Invalid hex character
		}
	}

	return address
}

// HasWitness returns true if the transaction has witness data
func (tx *RavenTransaction) HasWitness() bool {
	for _, input := range tx.Inputs {
		if len(input.ScriptWitness) > 0 {
			for _, witness := range input.ScriptWitness {
				if len(witness) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// IsCoinBase returns true if this is a coinbase transaction
func (tx *RavenTransaction) IsCoinBase() bool {
	return len(tx.Inputs) == 1 && tx.Inputs[0].PrevTxHash == (common.Hash{}) && tx.Inputs[0].PrevIndex == 0xFFFFFFFF
}

func (b *RavenBlock) verifyMerkleProofForCoinbaseTx() bool {
	// Calculate the hash of the coinbase transaction
	coinbaseHash := b.Coinbase.Hash()

	if len(b.MerkleProof) == 0 {
		// If no merkle proof, the coinbase hash should equal the merkle root
		return coinbaseHash == b.Header.MerkleRoot
	}

	// Iterate through the proof and compute the root
	computedHash := coinbaseHash
	for _, siblingHash := range b.MerkleProof {
		computedHash = hashRavenMerkleBranches(computedHash, siblingHash)
	}

	// Check if the computed hash matches the Merkle root
	return computedHash == b.Header.MerkleRoot
}

// writeCompactSize writes a variable-length integer as used in Bitcoin/Ravencoin protocol
func writeCompactSize(buf *bytes.Buffer, size uint64) {
	if size < 0xFD {
		buf.WriteByte(byte(size))
	} else if size <= 0xFFFF {
		buf.WriteByte(0xFD)
		binary.Write(buf, binary.LittleEndian, uint16(size))
	} else if size <= 0xFFFFFFFF {
		buf.WriteByte(0xFE)
		binary.Write(buf, binary.LittleEndian, uint32(size))
	} else {
		buf.WriteByte(0xFF)
		binary.Write(buf, binary.LittleEndian, size)
	}
}

func encodeMerkleProofRaven(proof []common.Hash) ([]byte, error) {
	var encodedProof [][]byte
	for _, hash := range proof {
		encodedProof = append(encodedProof, hash[:])
	}

	// Use RLP to encode the entire structure
	encodedBytes, err := rlp.EncodeToBytes(encodedProof)
	if err != nil {
		return nil, err
	}
	return encodedBytes, nil
}

func decodeMerkleProofRaven(data []byte) ([]common.Hash, error) {
	// Decode the raw RLP data into a slice of byte slices
	var decoded [][]byte
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		return nil, err
	}

	// Transform back into the desired structure
	var result []common.Hash
	for _, data := range decoded {
		if len(data) != 32 {
			return nil, fmt.Errorf("invalid hash size: expected 32 bytes, got %d", len(data))
		}
		result = append(result, common.BytesToHash(data))
	}
	return result, nil
}

// hashRavenMerkleBranches takes two hashes, treated as the left and right tree
// nodes, and returns the hash of their concatenation. This is a helper
// function used to aid in the generation of a merkle tree.
func hashRavenMerkleBranches(left, right common.Hash) common.Hash {
	// In Bitcoin/Ravencoin, the merkle tree hashing uses the raw hash bytes
	// but since our transaction hashes are already reversed for display,
	// we need to unreverse them for the merkle tree calculation

	// Convert display hashes back to internal byte order
	leftInternal := make([]byte, 32)
	rightInternal := make([]byte, 32)

	for i := 0; i < 32; i++ {
		leftInternal[i] = left[31-i]
		rightInternal[i] = right[31-i]
	}

	// Concatenate the left and right nodes in internal byte order
	combined := append(leftInternal[:], rightInternal[:]...)

	// Double SHA256 hash (Bitcoin-style)
	first := sha256.Sum256(combined)
	second := sha256.Sum256(first[:])

	// The result needs to be reversed back to display format
	reversedResult := make([]byte, 32)
	for i := 0; i < 32; i++ {
		reversedResult[i] = second[31-i]
	}

	return common.BytesToHash(reversedResult)
}

// buildMerkleTree builds a merkle tree from transaction hashes
// This follows Bitcoin/Ravencoin's merkle tree construction algorithm
func buildMerkleTree(txHashes []common.Hash) common.Hash {
	if len(txHashes) == 0 {
		return common.Hash{}
	}

	if len(txHashes) == 1 {
		return txHashes[0]
	}

	// Create a copy to avoid modifying the original slice
	hashes := make([]common.Hash, len(txHashes))
	copy(hashes, txHashes)

	// Keep building levels until we have one hash
	for len(hashes) > 1 {
		var nextLevel []common.Hash

		// Process pairs of hashes
		for i := 0; i < len(hashes); i += 2 {
			var left, right common.Hash
			left = hashes[i]

			// If odd number of hashes, duplicate the last one (Bitcoin/Ravencoin behavior)
			if i+1 < len(hashes) {
				right = hashes[i+1]
			} else {
				right = hashes[i] // Duplicate the last hash
			}

			// Hash the pair and add to next level
			combinedHash := hashRavenMerkleBranches(left, right)
			nextLevel = append(nextLevel, combinedHash)
		}

		hashes = nextLevel
	}

	return hashes[0]
}

func (block *RavenBlock) EncodeRLP(w io.Writer) error {
	merkleProof, err := encodeMerkleProofRaven(block.MerkleProof)
	if err != nil {
		return fmt.Errorf("failed to encode merkle proof: %w", err)
	}

	return rlp.Encode(w, []interface{}{
		block.Header,
		merkleProof,
		block.Coinbase,
		block.Hash,
	})
}

func (block *RavenBlock) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpRavenBlock
	if err := s.Decode(&decoded); err != nil {
		return fmt.Errorf("failed to decode raven block: %w", err)
	}

	block.Header = decoded.Header
	block.Coinbase = decoded.Coinbase
	block.Hash = decoded.Hash

	merkleProof, err := decodeMerkleProofRaven(decoded.MerkleProof)
	if err != nil {
		return fmt.Errorf("failed to decode raven block merkle proof: %w", err)
	}
	block.MerkleProof = merkleProof

	return nil
}

// bitsToTarget converts the compact "bits" representation to a big.Int target
// This implements the exact same logic as Ravencoin's arith_uint256::SetCompact() method
func bitsToTarget(bits uint32) *big.Int {
	// Extract the size (exponent) and word (mantissa) following Ravencoin's SetCompact
	nSize := int(bits >> 24)
	nWord := bits & 0x007fffff // Note: 0x007fffff not 0x00ffffff (excludes sign bit)

	var target *big.Int

	if nSize <= 3 {
		// For size <= 3, shift the word right
		nWord >>= uint(8 * (3 - nSize))
		target = big.NewInt(int64(nWord))
	} else {
		// For size > 3, shift the word left
		target = big.NewInt(int64(nWord))
		target.Lsh(target, uint(8*(nSize-3)))
	}

	// Handle negative values and overflow cases like Ravencoin
	// If the sign bit is set (0x00800000) and nWord != 0, it's negative
	if nWord != 0 && (bits&0x00800000) != 0 {
		// In Ravencoin, negative targets are invalid, return 0
		return big.NewInt(0)
	}

	// Check for overflow like Ravencoin does
	if nWord != 0 && ((nSize > 34) ||
		(nWord > 0xff && nSize > 33) ||
		(nWord > 0xffff && nSize > 32)) {
		// Overflow case, return 0 (invalid)
		return big.NewInt(0)
	}

	return target
}
