// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"

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

// BlockHeader defines information about a block and is used in the bitcoin
// block (MsgBlock) and headers (MsgHeaders) messages.
type KaspaBLockHeader struct {
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

func (header *KaspaBLockHeader) BlueScore() uint64 {
	return header.KblueScore
}

func (header *KaspaBLockHeader) PruningPoint() *externalapi.DomainHash {
	return header.KpruningPoint
}

func (header *KaspaBLockHeader) DAAScore() uint64 {
	return header.KdaaScore
}

func (header *KaspaBLockHeader) BlueWork() *big.Int {
	return header.KblueWork
}

func (header *KaspaBLockHeader) ToImmutable() externalapi.BlockHeader {
	return header.clone()
}

func (header *KaspaBLockHeader) SetNonce(nonce uint64) {
	header.Knonce = nonce
}

func (header *KaspaBLockHeader) SetTimeInMilliseconds(timeInMilliseconds int64) {
	header.Ktimestamp = uint64(timeInMilliseconds)
}

func (header *KaspaBLockHeader) SetHashMerkleRoot(hashMerkleRoot *externalapi.DomainHash) {
	header.KhashMerkleRoot = hashMerkleRoot
}

func (header *KaspaBLockHeader) Version() uint16 {
	return header.Kversion
}

func (header *KaspaBLockHeader) Parents() []externalapi.BlockLevelParents {
	return header.Kparents
}

func (header *KaspaBLockHeader) DirectParents() externalapi.BlockLevelParents {
	if len(header.Kparents) == 0 {
		return externalapi.BlockLevelParents{}
	}

	return header.Kparents[0]
}

func (header *KaspaBLockHeader) HashMerkleRoot() *externalapi.DomainHash {
	return header.KhashMerkleRoot
}

func (header *KaspaBLockHeader) AcceptedIDMerkleRoot() *externalapi.DomainHash {
	return header.KacceptedIDMerkleRoot
}

func (header *KaspaBLockHeader) UTXOCommitment() *externalapi.DomainHash {
	return header.KutxoCommitment
}

func (header *KaspaBLockHeader) TimeInMilliseconds() int64 {
	return int64(header.Ktimestamp)
}

func (header *KaspaBLockHeader) Bits() uint32 {
	return header.Kbits
}

func (header *KaspaBLockHeader) Nonce() uint64 {
	return header.Knonce
}

func (header *KaspaBLockHeader) Equal(other externalapi.BaseBlockHeader) bool {
	if header == nil || other == nil {
		return header == other
	}

	// If only the underlying value of other is nil it'll
	// make `other == nil` return false, so we check it
	// explicitly.
	downcastedOther := other.(*KaspaBLockHeader)
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

func (header *KaspaBLockHeader) clone() *KaspaBLockHeader {
	return &KaspaBLockHeader{
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

func (header *KaspaBLockHeader) ToMutable() externalapi.MutableBlockHeader {
	return header.clone()
}

func (header *KaspaBLockHeader) BlockLevel(maxBlockLevel int) int {
	return 0
}

// PowHash returns the litecoin scrypt hash of this block header. This value is
// used to check the PoW on blocks advertised on the network.
func (h *KaspaBLockHeader) PowHash() *externalapi.DomainHash {
	return consensushashing.HeaderHash(h)
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
) KaspaBLockHeader {
	return KaspaBLockHeader{
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
	Header      KaspaBLockHeader               `json:"header"`
	MerkleProof []*externalapi.DomainHash      `json:"merkleProof"` // merge proof path to verify the coinbase tx
	Coinbase    *externalapi.DomainTransaction `json:"coinbase"`
}

func (b *KaspaBlock) Chain() ParentChain {
	return KaspaChain
}

// Verify block's PoW
func (b *KaspaBlock) VerifyPoW() error {
	j, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fmt.Println(err)
	}
	fmt.Print(string(j))

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

// Verify block's PoW
func (b *KaspaBlock) VerifyCoinbase() bool {
	if !transactionhelper.IsCoinBase(b.Coinbase) {
		return false
	}

	// verify merke root
	return b.verifyMerkleProofForCoinbaseTx()
}

// Verify block's PoW
func (b *KaspaBlock) GetMinerAddress() (common.Address, error) {
	if b.Coinbase == nil {
		return common.Address{}, errors.New("kaspa coinbase transaction is nil")
	}

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
		computedHash.Equal(b.Header.HashMerkleRoot())
	}

	// Iterate through the proof and compute the root
	for _, siblingHash := range b.MerkleProof {
		computedHash = hashMerkleBranches(computedHash, siblingHash)
	}

	// Check if the computed hash matches the Merkle root
	return computedHash.Equal(b.Header.HashMerkleRoot())
}

type KaspaMiningTx struct {
	MergeMiningTx
	MergeProof KaspaBlock
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
