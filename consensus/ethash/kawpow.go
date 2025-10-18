package ethash

import (
	"encoding/binary"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

const (
	progpowCacheBytes   = 16 * 1024             // Total size 16*1024 bytes
	progpowCacheWords   = progpowCacheBytes / 4 // Total size 16*1024 bytes
	progpowLanes        = 16                    // The number of parallel lanes that coordinate to calculate a single hash instance.
	progpowRegs         = 32                    // The register file usage size
	progpowDagLoads     = 4                     // Number of uint32 loads from the DAG per lane
	progpowCntCache     = 11
	progpowCntMath      = 18
	progpowPeriodLength = 3            // Blocks per progpow epoch (N)
	progpowCntDag       = loopAccesses // Number of DAG accesses, same as ethash (64)
	progpowMixBytes     = 2 * mixBytes

	kawPowAlgorithmRevision = 194
)

var fnvOffsetBasis uint32 = 0x811c9dc5

var ravencoinKawpow = [15]uint32{
	0x00000072, // R
	0x00000041, // A
	0x00000056, // V
	0x00000045, // E
	0x0000004E, // N
	0x00000043, // C
	0x0000004F, // O
	0x00000049, // I
	0x0000004E, // N
	0x0000004B, // K
	0x00000041, // A
	0x00000057, // W
	0x00000050, // P
	0x0000004F, // O
	0x00000057, // W
}

func rotl32(x uint32, n uint32) uint32 {
	return ((x) << (n % 32)) | ((x) >> (32 - (n % 32)))
}

func rotr32(x uint32, n uint32) uint32 {
	return ((x) >> (n % 32)) | ((x) << (32 - (n % 32)))
}

func lower32(in uint64) uint32 {
	return uint32(in)
}

func higher32(in uint64) uint32 {
	return uint32(in >> 32)
}

var keccakfRNDC = [24]uint32{
	0x00000001, 0x00008082, 0x0000808a, 0x80008000, 0x0000808b, 0x80000001,
	0x80008081, 0x00008009, 0x0000008a, 0x00000088, 0x80008009, 0x8000000a,
	0x8000808b, 0x0000008b, 0x00008089, 0x00008003, 0x00008002, 0x00000080,
	0x0000800a, 0x8000000a, 0x80008081, 0x00008080, 0x80000001, 0x80008008}

func keccakF800Round(st *[25]uint32, r int) {
	var keccakfROTC = [24]uint32{1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2,
		14, 27, 41, 56, 8, 25, 43, 62, 18, 39, 61,
		20, 44}
	var keccakfPILN = [24]uint32{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24,
		4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9,
		6, 1}
	bc := make([]uint32, 5)
	// Theta
	for i := 0; i < 5; i++ {
		bc[i] = st[i] ^ st[i+5] ^ st[i+10] ^ st[i+15] ^ st[i+20]
	}

	for i := 0; i < 5; i++ {
		t := bc[(i+4)%5] ^ rotl32(bc[(i+1)%5], 1)
		for j := 0; j < 25; j += 5 {
			st[j+i] ^= t
		}
	}

	// Rho Pi
	t := st[1]
	for i, j := range keccakfPILN {
		bc[0] = st[j]
		st[j] = rotl32(t, keccakfROTC[i])
		t = bc[0]
	}

	//  Chi
	for j := 0; j < 25; j += 5 {
		bc[0] = st[j+0]
		bc[1] = st[j+1]
		bc[2] = st[j+2]
		bc[3] = st[j+3]
		bc[4] = st[j+4]
		st[j+0] ^= ^bc[1] & bc[2]
		st[j+1] ^= ^bc[2] & bc[3]
		st[j+2] ^= ^bc[3] & bc[4]
		st[j+3] ^= ^bc[4] & bc[0]
		st[j+4] ^= ^bc[0] & bc[1]
	}

	//  Iota
	st[0] ^= keccakfRNDC[r]
	//return st
}

// Generate the hash seed from the header hash and nonce, using keccak-f800 short
// Returns both the seed and the state2 for final hash computation
func generateHashSeed(headerHash []byte, nonce uint64) (uint64, [8]uint32) {
	var st [25]uint32

	// 1st: Header hash (8 words)
	for i := 0; i < 8; i++ {
		st[i] = binary.LittleEndian.Uint32(headerHash[4*i:])
	}

	// 2nd: Nonce (2 words)
	st[8] = uint32(nonce)
	st[9] = uint32(nonce >> 32)

	// 3rd: RavenCoin constants (15 words)
	for i := 10; i < 25; i++ {
		st[i] = ravencoinKawpow[i-10]
	}

	// Run Keccak - do all 22 rounds like the C++ version
	for r := 0; r < 22; r++ {
		keccakF800Round(&st, r)
	}

	// Extract seed and state2
	hashSeed := [2]uint32{st[0], st[1]}
	var state2 [8]uint32
	for i := 0; i < 8; i++ {
		state2[i] = st[i]
	}

	return uint64(hashSeed[1])<<32 | uint64(hashSeed[0]), state2
}

func keccakF800Long(state2 [8]uint32, result []uint32) []byte {
	// Use the provided state2 from the initial Keccak round
	// Now do the final Keccak round like C++ KawPoW
	var st [25]uint32

	// 1st: initial 8 words of state are kept as carry-over from initial keccak
	for i := 0; i < 8; i++ {
		st[i] = state2[i]
	}

	// 2nd: subsequent 8 words are carried from digest/mix
	for i := 8; i < 16; i++ {
		st[i] = result[i-8]
	}

	// 3rd: apply ravencoin input constraints
	for i := 16; i < 25; i++ {
		st[i] = ravencoinKawpow[i-16]
	}

	// Run keccak loop
	for r := 0; r < 22; r++ {
		keccakF800Round(&st, r)
	}

	ret := make([]byte, 32)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint32(ret[i*4:], st[i])
	}
	return ret
}

func fnv1a(h *uint32, d uint32) uint32 {
	*h = (*h ^ d) * uint32(0x1000193)
	return *h
}

func fnv1a_simple(h uint32, d uint32) uint32 {
	return (h ^ d) * uint32(0x1000193)
}

type kiss99State struct {
	z     uint32
	w     uint32
	jsr   uint32
	jcong uint32
}

func kiss99(st *kiss99State) uint32 {
	var MWC uint32
	st.z = 36969*(st.z&65535) + (st.z >> 16)
	st.w = 18000*(st.w&65535) + (st.w >> 16)
	MWC = ((st.z << 16) + st.w)
	st.jsr ^= (st.jsr << 17)
	st.jsr ^= (st.jsr >> 13)
	st.jsr ^= (st.jsr << 5)
	st.jcong = 69069*st.jcong + 1234567
	return ((MWC ^ st.jcong) + st.jsr)
}

func initMix(seed uint64) [progpowLanes][progpowRegs]uint32 {
	var mix [progpowLanes][progpowRegs]uint32

	// Match C++ init_mix pattern exactly
	z := fnv1a_simple(fnvOffsetBasis, lower32(seed)) // fnv1a(fnv_offset_basis, hash_seed[0])
	w := fnv1a_simple(z, higher32(seed))             // fnv1a(z, hash_seed[1])

	for laneId := uint32(0); laneId < progpowLanes; laneId++ {
		var st kiss99State
		jsr := fnv1a_simple(w, laneId)     // fnv1a(w, l)
		jcong := fnv1a_simple(jsr, laneId) // fnv1a(jsr, l)

		st.z = z
		st.w = w
		st.jsr = jsr
		st.jcong = jcong

		for i := 0; i < progpowRegs; i++ {
			mix[laneId][i] = kiss99(&st)
		}
	}

	return mix
}

// Merge new data from b into the value in a
// Assuming A has high entropy only do ops that retain entropy
// even if B is low entropy
// (IE don't do A&B)
func merge(a *uint32, b uint32, r uint32) {
	switch r % 4 {
	case 0:
		*a = (*a * 33) + b
	case 1:
		*a = (*a ^ b) * 33
	case 2:
		*a = rotl32(*a, ((r>>16)%31)+1) ^ b
	default:
		*a = rotr32(*a, ((r>>16)%31)+1) ^ b
	}
}

func kawpowInit(seed uint64) (kiss99State, [progpowRegs]uint32, [progpowRegs]uint32) {
	var randState kiss99State

	fnvHash := uint32(0x811c9dc5)

	randState.z = fnv1a(&fnvHash, lower32(seed))
	randState.w = fnv1a(&fnvHash, higher32(seed))
	randState.jsr = fnv1a(&fnvHash, lower32(seed))
	randState.jcong = fnv1a(&fnvHash, higher32(seed))

	// Create a random sequence of mix destinations for merge()
	// and mix sources for cache reads
	// guarantees every destination merged once
	// guarantees no duplicate cache reads, which could be optimized away
	// Uses Fisher-Yates shuffle
	var dstSeq = [32]uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	var srcSeq = [32]uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}

	for i := uint32(progpowRegs - 1); i > 0; i-- {
		j := kiss99(&randState) % (i + 1)
		dstSeq[i], dstSeq[j] = dstSeq[j], dstSeq[i]
		j = kiss99(&randState) % (i + 1)
		srcSeq[i], srcSeq[j] = srcSeq[j], srcSeq[i]
	}
	return randState, dstSeq, srcSeq
}

// Random math between two input values
func kawpowMath(a uint32, b uint32, r uint32) uint32 {
	switch r % 11 {
	case 0:
		return a + b
	case 1:
		return a * b
	case 2:
		return higher32(uint64(a) * uint64(b))
	case 3:
		if a < b {
			return a
		}
		return b
	case 4:
		return rotl32(a, b)
	case 5:
		return rotr32(a, b)
	case 6:
		return a & b
	case 7:
		return a | b
	case 8:
		return a ^ b
	case 9:
		return uint32(bits.LeadingZeros32(a) + bits.LeadingZeros32(b))
	case 10:
		return uint32(bits.OnesCount32(a) + bits.OnesCount32(b))

	default:
		return 0
	}
}

func round(seed uint64, loop uint32, mix *[progpowLanes][progpowRegs]uint32,
	lookup func(index uint32) []byte,
	cDag []uint32, datasetSize uint32) {
	// All lanes share a base address for the global load
	// Global offset uses mix[0] to guarantee it depends on the load result
	gOffset := mix[loop%progpowLanes][0] % (64 * datasetSize / (progpowLanes * progpowDagLoads))

	var (
		srcCounter = uint32(0)
		dstCounter = uint32(0)
		randState  kiss99State
		srcSeq     [32]uint32
		dstSeq     [32]uint32
		rnd        = kiss99
		//iMax       = uint32(0)
		index           = uint32(0)
		data_g []uint32 = make([]uint32, progpowDagLoads)
	)
	// 256 bytes of dag data
	dag_item := make([]byte, 256)
	// The lookup returns 64, so we'll fetch four items
	copy(dag_item, lookup((gOffset*progpowLanes)*progpowDagLoads))
	copy(dag_item[64:], lookup((gOffset*progpowLanes)*progpowDagLoads+16))
	copy(dag_item[128:], lookup((gOffset*progpowLanes)*progpowDagLoads+32))
	copy(dag_item[192:], lookup((gOffset*progpowLanes)*progpowDagLoads+48))

	// Lanes can execute in parallel and will be convergent
	for l := uint32(0); l < progpowLanes; l++ {

		// initialize the seed and mix destination sequence
		randState, dstSeq, srcSeq = kawpowInit(seed)
		srcCounter = uint32(0)
		dstCounter = uint32(0)

		for i := uint32(0); i < progpowCntMath; i++ {
			if i < progpowCntCache {
				// Cached memory access
				// lanes access random location

				src := srcSeq[(srcCounter)%progpowRegs]
				srcCounter++

				offset := mix[l][src] % progpowCacheWords
				data32 := cDag[offset]

				dst := dstSeq[(dstCounter)%progpowRegs]
				dstCounter++

				r := kiss99(&randState)
				merge(&mix[l][dst], data32, r)
			}

			//if i < progpowCntMath
			{
				// Random Math
				srcRnd := rnd(&randState) % (progpowRegs * (progpowRegs - 1))
				src1 := srcRnd % progpowRegs
				src2 := srcRnd / progpowRegs
				if src2 >= src1 {
					src2++
				}
				data32 := kawpowMath(mix[l][src1], mix[l][src2], rnd(&randState))

				dst := dstSeq[(dstCounter)%progpowRegs]
				dstCounter++

				merge(&mix[l][dst], data32, rnd(&randState))
			}
		}
		index = ((l ^ loop) % progpowLanes) * progpowDagLoads

		data_g[0] = binary.LittleEndian.Uint32(dag_item[4*index:])
		data_g[1] = binary.LittleEndian.Uint32(dag_item[4*(index+1):])
		data_g[2] = binary.LittleEndian.Uint32(dag_item[4*(index+2):])
		data_g[3] = binary.LittleEndian.Uint32(dag_item[4*(index+3):])

		merge(&mix[l][0], data_g[0], rnd(&randState))

		for i := 1; i < progpowDagLoads; i++ {
			dst := dstSeq[(dstCounter)%progpowRegs]
			dstCounter++
			merge(&mix[l][dst], data_g[i], rnd(&randState))
		}
	}
}

func kawpow(hash []byte, nonce uint64, size uint64, blockNumber uint64, cDag []uint32,
	lookup func(index uint32) []byte) ([]byte, []byte) {
	var (
		mix         [progpowLanes][progpowRegs]uint32
		laneResults [progpowLanes]uint32
	)
	result := make([]uint32, 8)
	seed, state2 := generateHashSeed(hash, nonce)
	mix = initMix(seed)

	period := (blockNumber / progpowPeriodLength)
	for l := uint32(0); l < progpowCntDag; l++ {
		round(period, l, &mix, lookup, cDag, uint32(size/progpowMixBytes))
	}

	// Reduce mix data to a single per-lane result
	for lane := uint32(0); lane < progpowLanes; lane++ {
		laneResults[lane] = fnvOffsetBasis
		for i := uint32(0); i < progpowRegs; i++ {
			fnv1a(&laneResults[lane], mix[lane][i])
		}
	}
	for i := uint32(0); i < 8; i++ {
		result[i] = fnvOffsetBasis
	}
	for lane := uint32(0); lane < progpowLanes; lane++ {
		fnv1a(&result[lane%8], laneResults[lane])
	}
	mixHash := make([]byte, 8*4)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint32(mixHash[i*4:], result[i])
	}

	finalHash := keccakF800Long(state2, result[:])

	return mixHash[:], finalHash[:]
}

func (ethash *Ethash) KawPowHash(number uint64, hash common.Hash, nonce uint64) (common.Hash, common.Hash) {
	cache := ethash.kawpowCache(number)
	size := datasetSize(number, kawpowEpochLength)

	keccak512 := makeHasher(sha3.NewLegacyKeccak512())
	lookup := func(index uint32) []byte {
		return generateDatasetItem(cache.cache, index/16, keccak512, kawpowDatasetParents)
	}

	mixHash, finalHash := kawpow(hash.Bytes(), nonce, size, number, cache.cDag, lookup)
	return common.BytesToHash(mixHash), common.BytesToHash(finalHash)
}
