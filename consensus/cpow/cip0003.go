/// Copyright 2026 The go-canxium Authors
/// This file is part of the go-canxium library.
///
/// The go-canxium library is free software: you can redistribute it and/or modify
/// it under the terms of the GNU Lesser General Public License as published by
/// the Free Software Foundation, either version 3 of the License, or
/// (at your option) any later version.
///
/// The go-canxium library is distributed in the hope that it will be useful,
/// but WITHOUT ANY WARRANTY; without even the implied warranty of
/// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
/// GNU Lesser General Public License for more details.
///
/// You should have received a copy of the GNU Lesser General Public License
/// along with the go-canxium library. If not, see <http://www.gnu.org/licenses/>.
///
/// This CIP implements the miner nonce management as specified in CIP-0003: PoW 2.0

package cpow

import (
	"bytes"
	"errors"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// WDC pre-deploy contract address
var WdcAddress = common.HexToAddress("0x0000000000000000000000000000000000003003")
var SystemTransactionSigner = common.HexToAddress("0x986CD7BdF659D92ac135229e800Df8b733dAe97a")
var SystemSignerPrivate = common.Hex2Bytes("da30f64caa77a9b16452d993fcaaae180121c5eeccd347a13020856a54a03ecd")

var (
	ErrInvalidWDCSystemSender = errors.New("invalid WDC system transaction sender")
	ErrInvalidWDCReceiver     = errors.New("invalid WDC system transaction receiver")
	ErrInvalidWDCNonce        = errors.New("invalid WDC system transaction nonce")
	ErrInvalidWDCInput        = errors.New("invalid WDC system transaction input data")
	ErrWDCNonceMismatch       = errors.New("WDC system transaction nonce argument mismatch")
	ErrWDCBlockMismatch       = errors.New("WDC system transaction block number argument mismatch")
	ErrBadSystemTx            = errors.New("bad WDC system transaction")
	ErrNoMinerForNonce        = errors.New("no miner found for given nonce in WDC cache")
)

const (
	WDCMinersArraySlot = 2
	// WDCStorageSlot is the position of 'minerNonces' in the contract variables.
	// Check your solidity variable ordering carefully!
	// 0: deposited, 1: lastRecalculatedEpoch, 2: miners array, 3: minerNonces mapping
	WDCMapSlot = 3
	// wdcCacheSize bounds the number of distinct state roots whose miner sets we
	// keep. A handful is enough: it deduplicates the several lookups done while
	// processing a single block and covers a few recent blocks / competing forks.
	wdcCacheSize = 16
)

// WDCStateReader is the minimal state access the WDC lookups need. *state.StateDB
// satisfies it; keeping the surface small also makes the logic unit-testable with
// a fake reader.
type WDCStateReader interface {
	GetState(addr common.Address, key common.Hash) common.Hash
}

// CachedMiner represents a single miner's assigned nonce range.
type CachedMiner struct {
	Index  uint64 // Index in the contract miners[] array
	Start  uint64
	End    uint64
	Miner  common.Address
	Signer common.Address
}

// MinerSet is an immutable snapshot of every miner's nonce range as recorded in
// the WDC contract at one particular state. It is derived directly from that
// state, so it is always correct for the block being processed regardless of
// epoch, fork, or sync mode.
type MinerSet struct {
	ranges []CachedMiner // sorted by Start nonce
	byAddr map[common.Address]CachedMiner
}

// ByNonce returns the miner whose assigned range contains nonce, or nil.
func (ms *MinerSet) ByNonce(nonce uint64) *CachedMiner {
	low, high := 0, len(ms.ranges)-1
	for low <= high {
		mid := (low + high) / 2
		entry := ms.ranges[mid]
		if nonce < entry.Start {
			high = mid - 1
		} else if nonce > entry.End {
			low = mid + 1
		} else {
			return &entry
		}
	}
	return nil
}

// ByAddress returns the miner registered under addr, or nil.
func (ms *MinerSet) ByAddress(addr common.Address) *CachedMiner {
	if entry, ok := ms.byAddr[addr]; ok {
		return &entry
	}
	return nil
}

// LoadMiners scans the WDC contract's miners[] array from the given state and
// builds a MinerSet. This is a pure function of the state; no shared mutable
// epoch snapshot is involved, so it is correct under mining, sync and reorg.
func LoadMiners(r WDCStateReader) *MinerSet {
	ms := &MinerSet{byAddr: make(map[common.Address]CachedMiner)}

	arrayLenHash := common.BigToHash(big.NewInt(WDCMinersArraySlot))
	arrayLen := new(big.Int).SetBytes(r.GetState(WdcAddress, arrayLenHash).Bytes()).Uint64()
	if arrayLen == 0 {
		return ms
	}

	ms.ranges = make([]CachedMiner, 0, arrayLen)
	arrayBaseSlot := crypto.Keccak256Hash(arrayLenHash[:])
	for i := uint64(0); i < arrayLen; i++ {
		elemSlot := common.BigToHash(new(big.Int).Add(arrayBaseSlot.Big(), new(big.Int).SetUint64(i)))
		minerAddr := common.BytesToAddress(r.GetState(WdcAddress, elemSlot).Bytes())
		if minerAddr == (common.Address{}) {
			continue
		}

		start, end, signer := readMinerData(r, minerAddr)
		if start == 0 && end == 0 {
			continue
		}

		entry := CachedMiner{Index: i, Start: start, End: end, Miner: minerAddr, Signer: signer}
		ms.ranges = append(ms.ranges, entry)
		ms.byAddr[minerAddr] = entry
	}

	sort.Slice(ms.ranges, func(i, j int) bool { return ms.ranges[i].Start < ms.ranges[j].Start })
	return ms
}

// WDCCache is a small, thread-safe LRU of MinerSets keyed by state root. The root
// is a free, fork-safe key: identical root ⇒ identical WDC storage ⇒ identical
// ranges. It only avoids recomputing LoadMiners for the same state; it never
// serves one state's ranges for another (the bug of the old epoch-keyed cache).
type WDCCache struct {
	sets *lru.Cache[common.Hash, *MinerSet]
}

// NewWDCCache creates the cache. Call this when initializing your consensus engine.
func NewWDCCache() *WDCCache {
	return &WDCCache{sets: lru.NewCache[common.Hash, *MinerSet](wdcCacheSize)}
}

// Miners returns the MinerSet for the state identified by root, deriving it from
// r on a cache miss. root must identify the state that r reads (e.g. parent.Root).
func (cache *WDCCache) Miners(root common.Hash, r WDCStateReader) *MinerSet {
	if ms, ok := cache.sets.Get(root); ok {
		return ms
	}
	ms := LoadMiners(r)
	cache.sets.Add(root, ms)
	return ms
}

// / Read miner data (start, end, signer) from state for a given miner address.
func readMinerData(state WDCStateReader, miner common.Address) (uint64, uint64, common.Address) {
	// ---------------------------------------------------------
	// 1. Calculate the Base Slot for minerNonces[miner]
	//    Formula: keccak256( abi.encode(miner) . abi.encode(map_slot) )
	// ---------------------------------------------------------

	// Create a 64-byte buffer for the key calculation
	// [0-31]: Key (Miner Address, padded)
	// [32-63]: Slot Index (2, padded)
	hasherBuf := make([]byte, 64)

	// Copy miner address into the lower 20 bytes of the first 32-byte word?
	// NO. Addresses are right-aligned in 32-byte words.
	// [00...00][address bytes]
	copy(hasherBuf[12:32], miner[:])

	// Put the Map Slot Index (2) into the second 32-byte word
	// [00...00][02]
	// We handle the big-endian writing manually or just set the last byte since it's small.
	hasherBuf[63] = byte(WDCMapSlot)

	// Hash it to get the Base Slot for this struct
	baseSlotHash := crypto.Keccak256Hash(hasherBuf)
	baseSlotInt := new(big.Int).SetBytes(baseSlotHash[:])

	// ---------------------------------------------------------
	// 2. Calculate the Target Slot (Base + 2)
	//    start/end are in the 3rd slot of the struct (offset 2)
	// ---------------------------------------------------------
	structOffset := big.NewInt(2)
	targetSlotInt := new(big.Int).Add(baseSlotInt, structOffset)
	targetSlotHash := common.BigToHash(targetSlotInt)

	// ---------------------------------------------------------
	// 3. Read Raw Bytes from StateDB
	// ---------------------------------------------------------
	data := state.GetState(WdcAddress, targetSlotHash)

	// If data is empty, the miner likely doesn't exist or hasn't been initialized
	if data == (common.Hash{}) {
		return 0, 0, common.Address{}
	}

	// ---------------------------------------------------------
	// 4. Unpack Packed Data
	//    Slot layout (Big Endian 32-byte word):
	//    [ ...unused... | end (8 bytes) | start (8 bytes) ]
	//
	//    Solidity "Lower Order Aligned" means:
	//    - start is at bits 0-63   (Last 8 bytes of the array)
	//    - end   is at bits 64-127 (Bytes 16-23 of the array)
	// ---------------------------------------------------------

	// Convert slot to BigInt for easier bitwise manipulation
	val := new(big.Int).SetBytes(data[:])

	// Mask: 0xFFFFFFFFFFFFFFFF (uint64 max)
	mask64 := new(big.Int).SetUint64(0xFFFFFFFFFFFFFFFF)

	// Extract START: (val >> 0) & mask
	startBig := new(big.Int).And(val, mask64)
	start := startBig.Uint64()

	// Extract END: (val >> 64) & mask
	val.Rsh(val, 64)
	endBig := new(big.Int).And(val, mask64)
	end := endBig.Uint64()

	// 5. Read Signer (Base + 5)
	signerSlot := common.BigToHash(new(big.Int).Add(baseSlotInt, big.NewInt(5)))
	signerData := state.GetState(WdcAddress, signerSlot)
	signer := common.BytesToAddress(signerData[:])

	return start, end, signer
}

// Create a system transaction to trigger the mined method on the WDC contract.
// Nonce and block is from the parent header
func CreateWDCMinedTx(config *params.ChainConfig, miners *MinerSet, nonce uint64, parentblock uint64) (*types.Transaction, error) {
	// Get miner index from the parent state's miner set
	miner := miners.ByNonce(nonce)
	if miner == nil {
		return nil, ErrNoMinerForNonce
	}

	// WDC mined method signature: mined(uint64 nonce, uint64 blockNumber)
	methodID := crypto.Keccak256([]byte("mined(uint64,uint64)"))[:4]

	// Prepare arguments
	nonceBytes := make([]byte, 32)
	minerIndexBytes := make([]byte, 32)

	// Fill bytes (big endian). Must use SetUint64, not NewInt(int64(nonce)),
	// because nonces > math.MaxInt64 would wrap to negative and FillBytes
	// would encode the absolute value, producing a mismatched argNonce.
	new(big.Int).SetUint64(nonce).FillBytes(nonceBytes)
	new(big.Int).SetUint64(miner.Index).FillBytes(minerIndexBytes)

	// ABI encode the parameters (padded to 32 bytes each)
	data := append(methodID, nonceBytes...)
	data = append(data, minerIndexBytes...)
	// Create the transaction
	tx := types.NewTransaction(
		parentblock,   // System transactions have nonce equal to block number minus one
		WdcAddress,    // To WDC contract
		big.NewInt(0), // No value transfer
		10000000,      // Gas limit
		big.NewInt(0), // Gas price 0: WDC system tx is cost-free (see Message.IsCostFree)
		data,          // Data payload
	)

	// Sign the transaction with the system signer private key.
	key, err := crypto.ToECDSA(SystemSignerPrivate)
	if err != nil {
		return nil, err
	}
	signer := types.NewEIP155Signer(config.ChainID)
	signedTx, err := types.SignTx(tx, signer, key)
	if err != nil {
		return nil, err
	}

	log.Info("Created WDC system transaction",
		"parent block", parentblock,
		"nonce", nonce,
		"miner index", miner.Index,
		"miner address", miner.Miner,
		"hash", signedTx.Hash(),
	)

	return signedTx, nil
}

// If the given transaction is a WDC system transaction, verify its correctness.
// System transaction is a function mined(uint64 nonceFound, uint256 minerIndex) call to the WDC contract.
// If not, make sure the sender is not a system transaction sender.
func VerifyWDCSystemTx(config *params.ChainConfig, miners *MinerSet, tx *types.Transaction, isLastTransaction bool, parent *types.Header) error {
	signer := types.MakeSigner(config, parent.Number)
	from, err := types.Sender(signer, tx)
	if err != nil {
		return err
	}

	if !isLastTransaction {
		if from == SystemTransactionSigner {
			return ErrInvalidWDCSystemSender
		}
		return nil
	} else if from != SystemTransactionSigner {
		return ErrInvalidWDCSystemSender
	}

	// Verify receiver
	if tx.To() == nil || *tx.To() != WdcAddress {
		return ErrInvalidWDCReceiver
	}

	// Verify nonce
	if tx.Nonce() != parent.Number.Uint64() {
		return ErrInvalidWDCNonce
	}

	// Verify data length
	if len(tx.Data()) != 4+32+32 {
		return ErrInvalidWDCInput
	}

	// Verify method ID
	methodID := crypto.Keccak256([]byte("mined(uint64,uint64)"))[:4]
	if !bytes.Equal(tx.Data()[:4], methodID) {
		return ErrInvalidWDCInput
	}

	// Extract and verify nonce argument
	argNonce := new(big.Int).SetBytes(tx.Data()[4 : 4+32]).Uint64()
	if argNonce != parent.Nonce.Uint64() {
		return ErrWDCNonceMismatch
	}

	// Get miner index from the parent state's miner set
	miner := miners.ByNonce(parent.Nonce.Uint64())
	if miner == nil {
		return ErrNoMinerForNonce
	}

	// Extract and verify miner index argument
	argMinerIndex := new(big.Int).SetBytes(tx.Data()[4+32 : 4+32+32]).Uint64()
	if argMinerIndex != miner.Index {
		return ErrWDCBlockMismatch
	}

	return nil
}
