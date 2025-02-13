// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/kaspanet/kaspad/domain/consensus/model/externalapi"
	"github.com/kaspanet/kaspad/domain/consensus/utils/consensushashing"
	"github.com/kaspanet/kaspad/domain/consensus/utils/hashes"
	"github.com/kaspanet/kaspad/domain/consensus/utils/pow"
	"github.com/kaspanet/kaspad/domain/consensus/utils/transactionhelper"
	"github.com/kaspanet/kaspad/util/difficulty"
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

// BlockHeader defines information about a block and is used in the bitcoin
// block (MsgBlock) and headers (MsgHeaders) messages.
type KaspaBlockHeader struct {
	// Version of the block. This is not the same as the protocol version.
	Kversion uint16 `json:"version"`

	// Parents are the parent block hashes of the block in the DAG per superblock level.
	Kparents []externalapi.BlockLevelParents `json:"parents"`

	// HashMerkleRoot is the merkle tree reference to hash of all transactions for the block.
	KhashMerkleRoot *externalapi.DomainHash `json:"hashMerkleRoot"`

	// AcceptedIDMerkleRoot is merkle tree reference to hash all transactions
	// accepted form the block.Blues
	KacceptedIDMerkleRoot *externalapi.DomainHash `json:"acceptedIDMerkleRoot"`

	// UTXOCommitment is an ECMH UTXO commitment to the block UTXO.
	KutxoCommitment *externalapi.DomainHash `json:"utxoCommitment"`

	// Time the block was created.
	Ktimestamp uint64 `json:"timestamp"`

	// Difficulty target for the block.
	Kbits uint32 `json:"bits"`

	// Nonce used to generate the block.
	Knonce uint64 `json:"nonce"`

	// DAASCore is the DAA score of the block.
	KdaaScore uint64 `json:"daaScore"`

	KblueScore uint64 `json:"blueScore"`

	// BlueWork is the blue work of the block.
	KblueWork *big.Int `json:"blueWork"`

	KpruningPoint *externalapi.DomainHash `json:"pruningPoint"`
}

type RlpKaspaBlockHeader struct {
	// Version of the block. This is not the same as the protocol version.
	Version uint16
	// Parents are the parent block hashes of the block in the DAG per superblock level.
	Parents []byte

	// HashMerkleRoot is the merkle tree reference to hash of all transactions for the block.
	HashMerkleRoot []byte

	// AcceptedIDMerkleRoot is merkle tree reference to hash all transactions
	// accepted form the block.Blues
	AcceptedIDMerkleRoot []byte

	// UTXOCommitment is an ECMH UTXO commitment to the block UTXO.
	UtxoCommitment []byte

	// Time the block was created.
	Timestamp uint64

	// Difficulty target for the block.
	Bits uint32

	// Nonce used to generate the block.
	Nonce uint64

	// DAASCore is the DAA score of the block.
	DaaScore uint64

	BlueScore uint64

	// BlueWork is the blue work of the block.
	BlueWork *big.Int

	PruningPoint []byte
}

func (header *KaspaBlockHeader) BlueScore() uint64 {
	return header.KblueScore
}

func (header *KaspaBlockHeader) PruningPoint() *externalapi.DomainHash {
	return header.KpruningPoint
}

func (header *KaspaBlockHeader) DAAScore() uint64 {
	return header.KdaaScore
}

func (header *KaspaBlockHeader) BlueWork() *big.Int {
	return header.KblueWork
}

func (header *KaspaBlockHeader) ToImmutable() externalapi.BlockHeader {
	return header.clone()
}

func (header *KaspaBlockHeader) SetNonce(nonce uint64) {
	header.Knonce = nonce
}

func (header *KaspaBlockHeader) SetTimeInMilliseconds(timeInMilliseconds int64) {
	header.Ktimestamp = uint64(timeInMilliseconds)
}

func (header *KaspaBlockHeader) SetHashMerkleRoot(hashMerkleRoot *externalapi.DomainHash) {
	header.KhashMerkleRoot = hashMerkleRoot
}

func (header *KaspaBlockHeader) Version() uint16 {
	return header.Kversion
}

func (header *KaspaBlockHeader) Parents() []externalapi.BlockLevelParents {
	return header.Kparents
}

func (header *KaspaBlockHeader) DirectParents() externalapi.BlockLevelParents {
	if len(header.Kparents) == 0 {
		return externalapi.BlockLevelParents{}
	}

	return header.Kparents[0]
}

func (header *KaspaBlockHeader) HashMerkleRoot() *externalapi.DomainHash {
	return header.KhashMerkleRoot
}

func (header *KaspaBlockHeader) AcceptedIDMerkleRoot() *externalapi.DomainHash {
	return header.KacceptedIDMerkleRoot
}

func (header *KaspaBlockHeader) UTXOCommitment() *externalapi.DomainHash {
	return header.KutxoCommitment
}

func (header *KaspaBlockHeader) TimeInMilliseconds() int64 {
	return int64(header.Ktimestamp)
}

func (header *KaspaBlockHeader) Bits() uint32 {
	return header.Kbits
}

func (header *KaspaBlockHeader) Nonce() uint64 {
	return header.Knonce
}

func (header *KaspaBlockHeader) Equal(other externalapi.BaseBlockHeader) bool {
	if header == nil || other == nil {
		return header == other
	}

	// If only the underlying value of other is nil it'll
	// make `other == nil` return false, so we check it
	// explicitly.
	downcastedOther := other.(*KaspaBlockHeader)
	if header == nil || downcastedOther == nil {
		return header == downcastedOther
	}

	if header.Kversion != other.Version() {
		return false
	}

	if !externalapi.ParentsEqual(header.Parents(), other.Parents()) {
		return false
	}

	if !header.HashMerkleRoot().Equal(other.HashMerkleRoot()) {
		return false
	}

	if !header.AcceptedIDMerkleRoot().Equal(other.AcceptedIDMerkleRoot()) {
		return false
	}

	if !header.UTXOCommitment().Equal(other.UTXOCommitment()) {
		return false
	}

	if header.TimeInMilliseconds() != other.TimeInMilliseconds() {
		return false
	}

	if header.Bits() != other.Bits() {
		return false
	}

	if header.Nonce() != other.Nonce() {
		return false
	}

	if header.DAAScore() != other.DAAScore() {
		return false
	}

	if header.BlueScore() != other.BlueScore() {
		return false
	}

	if header.BlueWork().Cmp(other.BlueWork()) != 0 {
		return false
	}

	if !header.PruningPoint().Equal(other.PruningPoint()) {
		return false
	}

	return true
}

func (header *KaspaBlockHeader) clone() *KaspaBlockHeader {
	return &KaspaBlockHeader{
		Kversion:              header.Kversion,
		Kparents:              externalapi.CloneParents(header.Kparents),
		KhashMerkleRoot:       header.KhashMerkleRoot,
		KacceptedIDMerkleRoot: header.KacceptedIDMerkleRoot,
		KutxoCommitment:       header.KutxoCommitment,
		Ktimestamp:            header.Ktimestamp,
		Kbits:                 header.Kbits,
		Knonce:                header.Knonce,
		KdaaScore:             header.KdaaScore,
		KblueScore:            header.KblueScore,
		KblueWork:             header.KblueWork,
		KpruningPoint:         header.KpruningPoint,
	}
}

func (header *KaspaBlockHeader) ToMutable() externalapi.MutableBlockHeader {
	return header.clone()
}

func (header *KaspaBlockHeader) BlockLevel(maxBlockLevel int) int {
	return 0
}

// PowHash returns the litecoin scrypt hash of this block header. This value is
// used to check the PoW on blocks advertised on the network.
func (h *KaspaBlockHeader) PowHash() *externalapi.DomainHash {
	return consensushashing.HeaderHash(h)
}

func encodeBlockLevelParentsList(parents []externalapi.BlockLevelParents) ([]byte, error) {
	// Prepare a representation of the parents for RLP encoding
	var encodedParentsList [][][]byte
	for _, levelParents := range parents {
		var encodedLevel [][]byte
		for _, parent := range levelParents {
			if parent == nil {
				encodedLevel = append(encodedLevel, nil)
			} else {
				encodedLevel = append(encodedLevel, parent.ByteSlice())
			}
		}
		encodedParentsList = append(encodedParentsList, encodedLevel)
	}

	// Use RLP to encode the entire structure
	encodedBytes, err := rlp.EncodeToBytes(encodedParentsList)
	if err != nil {
		return nil, err
	}
	return encodedBytes, nil
}

func decodeBlockLevelParentsList(data []byte) ([]externalapi.BlockLevelParents, error) {
	// Decode the raw RLP data into a nested slice of byte slices
	var decoded [][][]byte
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		return nil, err
	}

	// Transform back into the desired structure
	var result []externalapi.BlockLevelParents
	for _, level := range decoded {
		var levelParents externalapi.BlockLevelParents
		for _, data := range level {
			if len(data) == 0 {
				levelParents = append(levelParents, nil)
			} else if len(data) != externalapi.DomainHashSize {
				return nil, fmt.Errorf("invalid DomainHash size: expected %d bytes, got %d", externalapi.DomainHashSize, len(data))
			} else {
				var hashArray [32]byte
				copy(hashArray[:], data)
				parent := externalapi.NewDomainHashFromByteArray(&hashArray)
				levelParents = append(levelParents, parent)
			}
		}
		result = append(result, levelParents)
	}
	return result, nil
}

func encodeDomainHash(domainHash *externalapi.DomainHash) []byte {
	if domainHash == nil {
		return nil
	}
	return domainHash.ByteSlice()
}

func decodeDomainHash(data []byte) (*externalapi.DomainHash, error) {
	if len(data) != 32 {
		return nil, fmt.Errorf("invalid data size: expected 32 bytes, got %d", len(data))
	}

	var hashArray [32]byte
	copy(hashArray[:], data)
	return externalapi.NewDomainHashFromByteArray(&hashArray), nil
}

func (header *KaspaBlockHeader) EncodeRLP(w io.Writer) error {
	parents, err := encodeBlockLevelParentsList(header.Kparents)
	if err != nil {
		return fmt.Errorf("failed to encode parents: %w", err)
	}

	// Encode all fields as an RLP list
	return rlp.Encode(w, []interface{}{
		header.Kversion,
		parents,
		encodeDomainHash(header.KhashMerkleRoot),
		encodeDomainHash(header.KacceptedIDMerkleRoot),
		encodeDomainHash(header.KutxoCommitment),
		header.Ktimestamp,
		header.Kbits,
		header.Knonce,
		header.KdaaScore,
		header.KblueScore,
		header.KblueWork,
		encodeDomainHash(header.KpruningPoint),
	})
}

func (header *KaspaBlockHeader) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpKaspaBlockHeader
	if err := s.Decode(&decoded); err != nil {
		return fmt.Errorf("failed to decode kaspa block header: %w", err)
	}

	header.Kversion = decoded.Version
	parents, err := decodeBlockLevelParentsList(decoded.Parents)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa block parents: %w", err)
	}

	header.Kparents = parents
	header.Ktimestamp = decoded.Timestamp
	header.Kbits = decoded.Bits
	header.Knonce = decoded.Nonce
	header.KdaaScore = decoded.DaaScore
	header.KblueScore = decoded.BlueScore
	header.KblueWork = decoded.BlueWork

	header.KhashMerkleRoot, err = decodeDomainHash(decoded.HashMerkleRoot)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa domain hash: %w", err)
	}
	header.KacceptedIDMerkleRoot, err = decodeDomainHash(decoded.AcceptedIDMerkleRoot)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa domain hash: %w", err)
	}
	header.KutxoCommitment, err = decodeDomainHash(decoded.UtxoCommitment)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa domain hash: %w", err)
	}
	header.KpruningPoint, err = decodeDomainHash(decoded.PruningPoint)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa domain hash: %w", err)
	}

	return nil
}

// NewImmutableBlockHeader returns a new immutable header
func NewImmutableKaspaBlockHeader(
	version uint16,
	parents []externalapi.BlockLevelParents,
	hashMerkleRoot *externalapi.DomainHash,
	acceptedIDMerkleRoot *externalapi.DomainHash,
	utxoCommitment *externalapi.DomainHash,
	timeInMilliseconds int64,
	bits uint32,
	nonce uint64,
	daaScore uint64,
	blueScore uint64,
	blueWork *big.Int,
	pruningPoint *externalapi.DomainHash,
) KaspaBlockHeader {
	return KaspaBlockHeader{
		Kversion:              version,
		Kparents:              parents,
		KhashMerkleRoot:       hashMerkleRoot,
		KacceptedIDMerkleRoot: acceptedIDMerkleRoot,
		KutxoCommitment:       utxoCommitment,
		Ktimestamp:            uint64(timeInMilliseconds),
		Kbits:                 bits,
		Knonce:                nonce,
		KdaaScore:             daaScore,
		KblueScore:            blueScore,
		KblueWork:             blueWork,
		KpruningPoint:         pruningPoint,
	}
}

type KaspaBlock struct {
	Header      *KaspaBlockHeader              `json:"header"`
	MerkleProof []*externalapi.DomainHash      `json:"merkleProof"` // merge proof path to verify the coinbase tx
	Coinbase    *externalapi.DomainTransaction `json:"coinbase"`
}

type RlpKaspaBlock struct {
	Header      *KaspaBlockHeader
	MerkleProof []byte
	Coinbase    *externalapi.DomainTransaction
}

func (b *KaspaBlock) Chain() CrossChain {
	return KaspaChain
}

func (b *KaspaBlock) PoWAlgorithm() PoWAlgorithm {
	return KHeavyHashAlgorithm
}

// IsValidBlock check to see if this is a valid kaspa block, header and coinbase are valid
func (b *KaspaBlock) IsValidBlock() bool {
	if b.Header == nil {
		return false
	}
	if b.Coinbase == nil {
		return false
	}
	if b.Header.Knonce == 0 || b.Header.Ktimestamp == 0 || b.Header.Kbits == 0 {
		return false
	}
	if len(b.Coinbase.Payload) == 0 {
		return false
	}
	return true
}

func (b *KaspaBlock) Copy() CrossChainBlock {
	header := b.Header.clone()
	coinbase := b.Coinbase.Clone()
	clonedProof := make([]*externalapi.DomainHash, len(b.MerkleProof))
	for i, hash := range b.MerkleProof {
		if hash != nil {
			// Deep copy each *DomainHash to avoid sharing memory
			clonedHash := *hash // Dereference to copy the value
			clonedProof[i] = &clonedHash
		}
	}

	block := KaspaBlock{
		Header:      header,
		MerkleProof: clonedProof,
		Coinbase:    coinbase,
	}

	return &block
}

func (b *KaspaBlock) BlockHash() string {
	hash := b.Header.PowHash()
	return hash.String()
}

func (b *KaspaBlock) Timestamp() uint64 {
	return uint64(b.Header.TimeInMilliseconds())
}

// Verify block's PoW
func (b *KaspaBlock) VerifyPoW() error {
	// The target difficulty must be larger than zero.
	state := pow.NewState(b.Header.ToMutable())
	target := &state.Target
	if target.Sign() <= 0 {
		return fmt.Errorf("kaspa merge block target difficulty of %064x is too low", target)
	}

	// The target difficulty must be less than the maximum allowed.
	if target.Cmp(mainPowMax) > 0 {
		return fmt.Errorf("kaspa merge block target difficulty of %064x is higher than max of %064x", target, mainPowMax)
	}

	// The block pow must be valid unless the flag to avoid proof of work checks is set.
	valid := state.CheckProofOfWork()
	if !valid {
		return errors.New("kaspa block has invalid proof of work")
	}

	return nil
}

func (b *KaspaBlock) Difficulty() *big.Int {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number. Note this is not the same as the proof of
	// work limit directly because the block difficulty is encoded in a block
	// with the compact form which loses precision.
	target := difficulty.CompactToBig(b.Header.Kbits)

	difficulty := new(big.Rat).SetFrac(mainPowMax, target)
	diff, _ := difficulty.Float64()

	roundingPrecision := float64(100)
	diff = math.Round(diff*roundingPrecision) / roundingPrecision

	return big.NewInt(int64(diff))
}

func (b *KaspaBlock) PowNonce() uint64 {
	return b.Header.Knonce
}

// VerifyCoinbase verify kaspa block coin base transaction
func (b *KaspaBlock) VerifyCoinbase() bool {
	if !transactionhelper.IsCoinBase(b.Coinbase) {
		return false
	}
	// verify merke root
	return b.verifyMerkleProofForCoinbaseTx()
}

// GetMinerAddress return canxium miner of a kaspa block
func (b *KaspaBlock) GetMinerAddress() (common.Address, error) {
	payload := b.Coinbase.Payload
	tagLength := len(minerTagPrefix) + 40 // 40 characters for the address
	if len(payload) < tagLength {
		// Payload is too short to contain a valid tag
		return zeroAddress, errors.New("invalid kaspa coinbase transaction payload length, can't get canxium miner address")
	}

	// Extract the last part of the payload
	tag := string(payload[len(payload)-tagLength:])

	// Validate the prefix
	if !strings.HasPrefix(tag, minerTagPrefix) {
		return zeroAddress, errors.New("invalid kaspa coinbase transaction payload, can't get canxium miner address tag")
	}

	address := strings.Replace(tag, minerTagPrefix, "0x", 1)
	return common.HexToAddress(address), nil
}

func (b *KaspaBlock) verifyMerkleProofForCoinbaseTx() bool {
	computedHash := consensushashing.TransactionHash(b.Coinbase)
	if len(b.MerkleProof) == 0 {
		return computedHash.Equal(b.Header.HashMerkleRoot())
	}

	// Iterate through the proof and compute the root
	for _, siblingHash := range b.MerkleProof {
		if siblingHash == nil {
			return false
		}
		computedHash = hashMerkleBranches(computedHash, siblingHash)
	}

	// Check if the computed hash matches the Merkle root
	return computedHash.Equal(b.Header.HashMerkleRoot())
}

func encodeMerkleProof(proof []*externalapi.DomainHash) ([]byte, error) {
	var encodedProof [][]byte
	for _, hash := range proof {
		encodedProof = append(encodedProof, hash.ByteSlice())
	}

	// Use RLP to encode the entire structure
	encodedBytes, err := rlp.EncodeToBytes(encodedProof)
	if err != nil {
		return nil, err
	}
	return encodedBytes, nil
}

func (block *KaspaBlock) EncodeRLP(w io.Writer) error {
	mergeProof, err := encodeMerkleProof(block.MerkleProof)
	if err != nil {
		return fmt.Errorf("failed to encode parents: %w", err)
	}

	// Encode all fields as an RLP list
	return rlp.Encode(w, []interface{}{
		block.Header,
		mergeProof,
		block.Coinbase,
	})
}

func decodeMerkleProof(data []byte) ([]*externalapi.DomainHash, error) {
	// Decode the raw RLP data into a nested slice of byte slices
	var decoded [][]byte
	if err := rlp.DecodeBytes(data, &decoded); err != nil {
		return nil, err
	}

	// Transform back into the desired structure
	var result []*externalapi.DomainHash
	for _, data := range decoded {

		var hashArray [32]byte
		copy(hashArray[:], data)
		hash := externalapi.NewDomainHashFromByteArray(&hashArray)
		result = append(result, hash)
	}
	return result, nil
}

func (block *KaspaBlock) DecodeRLP(s *rlp.Stream) error {
	var decoded RlpKaspaBlock
	if err := s.Decode(&decoded); err != nil {
		return fmt.Errorf("failed to decode kaspa block: %w", err)
	}

	block.Header = decoded.Header
	block.Coinbase = decoded.Coinbase
	merkleProof, err := decodeMerkleProof(decoded.MerkleProof)
	if err != nil {
		return fmt.Errorf("failed to decode kaspa block merkle proof: %w", err)
	}
	block.MerkleProof = merkleProof

	return nil
}

// hashMerkleBranches takes two hashes, treated as the left and right tree
// nodes, and returns the hash of their concatenation. This is a helper
// function used to aid in the generation of a merkle tree.
func hashMerkleBranches(left, right *externalapi.DomainHash) *externalapi.DomainHash {
	// Concatenate the left and right nodes.
	w := hashes.NewMerkleBranchHashWriter()

	w.InfallibleWrite(left.ByteSlice())
	w.InfallibleWrite(right.ByteSlice())

	return w.Finalize()
}

// CloneHashes returns a clone of the given hashes slice.
// Note: since DomainHash is a read-only type, the clone is shallow
func CloneHashes(hashes []*common.Hash) []*common.Hash {
	clone := make([]*common.Hash, len(hashes))
	copy(clone, hashes)
	return clone
}

// CloneParents creates a clone of the given BlockLevelParents slice
func CloneParents(parents [][]*common.Hash) [][]*common.Hash {
	clone := make([][]*common.Hash, len(parents))
	for i, blockLevelParents := range parents {
		clone[i] = CloneHashes(blockLevelParents)
	}
	return clone
}
