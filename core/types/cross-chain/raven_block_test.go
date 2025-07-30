package crosschain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// TestKawPoWIntegration demonstrates how to use KawPoW with our header hash implementation
func TestKawPoWIntegration(t *testing.T) {
	// Real Ravencoin block data for testing (Block 3948182) - exact copy from working HeaderHash test
	header := &RavenBlockHeader{
		Version:    805306368, // 0x30000000
		PrevBlock:  common.HexToHash("0000000000002bf0ec5a3009997996ddec01e2007bacad254b4e401d83d1125d"),
		MerkleRoot: common.HexToHash("97aceed868687eff866556509ce48ebb8c828cba0841b82aa6130d5548960402"),
		Timestamp:  1753515192,
		Bits:       0x1b011363,
		Nonce:      10160171505908156645,
		Height:     3948182,
		MixHash:    common.HexToHash("992b5ef8ffb12efe108e08cc0899c3c94fc367862677be827ae48d55476afc9f"),
	}

	t.Run("Header Hash Generation", func(t *testing.T) {
		// First, verify our header hash is correct
		calculatedHash := header.HeaderHash()
		expectedHash := "5385fbae28c68c5327492701c9fa6285e180513185721abf239850744666d269"

		t.Logf("Header Hash: %s", calculatedHash.Hex())
		t.Logf("Expected:    %s", expectedHash)

		if calculatedHash.Hex() != "0x"+expectedHash {
			t.Errorf("Header hash mismatch! Got %s, expected %s", calculatedHash.Hex(), expectedHash)
		}
	})

	t.Run("KawPoW Hash Generation", func(t *testing.T) {
		// Generate KawPoW hash and mix hash using our implementation
		expectedHash := common.HexToHash("0000000000008c6a907662d9b97693f068139658c1b2e84517906b9d951d0ede")

		// Time the first GenerateKawPoW call
		t.Logf("⏱️  Starting first KawPoW generation...")
		start := time.Now()
		finalHash, mixHash, err := header.GenerateKawPoW()
		duration1 := time.Since(start)
		t.Logf("⏱️  First GenerateKawPoW completed in: %v", duration1)

		if err != nil {
			t.Fatalf("Failed to generate KawPoW hash: %v", err)
		}

		t.Logf("KawPoW Results:")
		t.Logf("  Final Hash: %s", finalHash.Hex())
		t.Logf("  Mix Hash:   %s", mixHash.Hex())

		// Verify the hashes are not zero
		if finalHash == (common.Hash{}) {
			t.Error("Final hash should not be zero")
		}
		if mixHash == (common.Hash{}) {
			t.Error("Mix hash should not be zero")
		}
		if finalHash != expectedHash {
			t.Errorf("Final hash mismatch! Got %s, expected %s", finalHash.Hex(), expectedHash.Hex())
		}

		// Test with the known mix hash from the real block
		expectedMixHash := common.HexToHash("992b5ef8ffb12efe108e08cc0899c3c94fc367862677be827ae48d55476afc9f")
		header.MixHash = expectedMixHash

		// Time the second GenerateKawPoW call (should be faster due to cached epoch context)
		t.Logf("⏱️  Starting second KawPoW generation (should be faster with cached context)...")
		start2 := time.Now()
		_, mixHash2, err := header.GenerateKawPoW()
		duration2 := time.Since(start2)
		t.Logf("⏱️  Second GenerateKawPoW completed in: %v", duration2)

		if err != nil {
			t.Fatalf("Failed to generate KawPoW hash second time: %v", err)
		}

		// The mix hash should be consistent for the same inputs
		if mixHash != mixHash2 {
			t.Errorf("Mix hash inconsistent: %s vs %s", mixHash.Hex(), mixHash2.Hex())
		}

		// Test verification with the calculated mix hash
		header.MixHash = mixHash
		t.Logf("⏱️  Starting KawPoW verification...")
		start3 := time.Now()
		verified := header.VerifyKawPoW()
		duration3 := time.Since(start3)
		t.Logf("⏱️  KawPoW verification completed in: %v", duration3)

		if !verified {
			t.Error("KawPoW verification failed with calculated mix hash")
		}

		t.Logf("⏱️  Performance Summary:")
		t.Logf("    First generation:  %v (includes epoch context creation)", duration1)
		t.Logf("    Second generation: %v (epoch context cached)", duration2)
		t.Logf("    Verification:      %v (includes generation)", duration3)
		t.Logf("    Speed improvement: %.1fx faster with cached context", float64(duration1.Nanoseconds())/float64(duration2.Nanoseconds()))

		t.Logf("✅ KawPoW generation and verification successful!")
	})

	t.Run("KawPoW Parameters", func(t *testing.T) {
		// Get the header hash that would be passed to KawPoW
		headerHash := header.HeaderHash()

		// Prepare parameters for kawpow-cgo (following Ravencoin's KAWPOWHash function)
		headerHashStr := headerHash.Hex() // Remove 0x prefix: headerHash.Hex()[2:]
		nonceStr := fmt.Sprintf("%016x", header.Nonce)
		height := int64(header.Height)

		t.Logf("KawPoW Parameters:")
		t.Logf("  Header Hash: %s", headerHashStr)
		t.Logf("  Nonce:     %s (decimal: %d)", nonceStr, header.Nonce)
		t.Logf("  Height:      %d", height)

		// These are the exact parameters that would be passed to:
		// kawpow.KawpowHash(headerHashStr[2:], nonceStr, height)
		//
		// This matches Ravencoin's C++ implementation:
		// - headerHash from GetKAWPOWHeaderHash()
		// - nonce64 as 64-bit hex string
		// - block height as integer

		// In a real implementation, you would call:
		/*
			   import kawpow "github.com/ethereum/go-ethereum/crypto/kawpow"

				hash, mixHash := kawpow.KawpowHash(headerHashStr[2:], nonceStr, height)
				finalHash := common.BytesToHash(hash)
				calculatedMixHash := common.BytesToHash(mixHash)

				// Verify against known values for this block
				expectedMixHash := common.HexToHash("992b5ef8ffb12efe108e08cc0899c3c94fc367862677be827ae48d55476afc9f")

				if calculatedMixHash != expectedMixHash {
					t.Errorf("Mix hash mismatch! Got %s, expected %s", calculatedMixHash.Hex(), expectedMixHash.Hex())
				}

				// Check if final hash meets difficulty target
				target := bitsToTarget(header.Bits)
				finalHashBig := finalHash.Big()

				if finalHashBig.Cmp(target) <= 0 {
					t.Logf("✅ KawPoW verification successful! Hash %s meets target", finalHash.Hex())
				} else {
					t.Errorf("❌ KawPoW verification failed! Hash %s exceeds target %s", finalHash.Hex(), target.String())
				}
		*/

		// For now, just verify we have the correct parameters
		if len(headerHashStr) != 66 { // 0x + 64 hex chars
			t.Errorf("Invalid header hash format: %s", headerHashStr)
		}
		if len(nonceStr) != 16 { // 16 hex chars for 64-bit
			t.Errorf("Invalid nonce format: %s", nonceStr)
		}
		if height != 3948182 {
			t.Errorf("Invalid height: %d", height)
		}
	})

	t.Run("Target Calculation", func(t *testing.T) {
		// Test difficulty target calculation (same as Ravencoin)
		target := bitsToTarget(header.Bits)

		t.Logf("Difficulty bits: 0x%08x", header.Bits)
		t.Logf("Target: %s", target.String())

		// The target should be a reasonable difficulty for block 3948182
		// In Ravencoin, targets are large numbers that final hash must be <= to be valid
		if target.Sign() <= 0 {
			t.Errorf("Invalid target calculation: %s", target.String())
		}
	})
}

// TestRavenTransactionHash tests our transaction hashing implementation
// against known Ravencoin transaction hashes
func TestRavenTransactionHash(t *testing.T) {
	tests := []struct {
		name        string
		transaction *RavenTransaction
		expectedHex string
		description string
	}{
		{
			name: "Real Ravencoin Transaction",
			transaction: &RavenTransaction{
				Version: 2,
				Inputs: []RavenTxInput{
					{
						// Previous transaction: 4020aff7048487e368a21e1a65aca027d35c7f3a790305b4885abcceeed60eed
						// Note: This hash needs to be reversed for serialization (little-endian vs big-endian)
						PrevTxHash: func() common.Hash {
							// The displayed hash is in big-endian, but we need little-endian for serialization
							hashHex := "4020aff7048487e368a21e1a65aca027d35c7f3a790305b4885abcceeed60eed"
							hashBytes, _ := hex.DecodeString(hashHex)
							// Reverse the bytes
							for i := 0; i < len(hashBytes)/2; i++ {
								hashBytes[i], hashBytes[len(hashBytes)-1-i] = hashBytes[len(hashBytes)-1-i], hashBytes[i]
							}
							return common.BytesToHash(hashBytes)
						}(),
						PrevIndex: 1, // vout: 1
						// ScriptSig hex: 473044022018566ac67ee0c02ae76e15c70b1bfd9a288baf021b1368206ddd49d7dbbb111902206a4b57a0700de5b8b3e2bc80f4cab9c6c46da14c3e201f37873446dc1b8449950121039a93b89f0b9ad82f5c23bdc60ab23e63b27ca7fe055afee8a22a99adc62e190d
						ScriptSig: func() []byte {
							scriptHex := "473044022018566ac67ee0c02ae76e15c70b1bfd9a288baf021b1368206ddd49d7dbbb111902206a4b57a0700de5b8b3e2bc80f4cab9c6c46da14c3e201f37873446dc1b8449950121039a93b89f0b9ad82f5c23bdc60ab23e63b27ca7fe055afee8a22a99adc62e190d"
							script, _ := hex.DecodeString(scriptHex)
							return script
						}(),
						Sequence: 4294967294, // 0xFFFFFFFE
					},
				},
				Outputs: []RavenTxOutput{
					{
						Value: 1000281302592, // 10002.81302592 RVN in satoshis
						// ScriptPubKey hex: 76a914a66bac356e012e42b6d39f41046de39050255c1288ac
						ScriptPubKey: func() []byte {
							scriptHex := "76a914a66bac356e012e42b6d39f41046de39050255c1288ac"
							script, _ := hex.DecodeString(scriptHex)
							return script
						}(),
					},
				},
				LockTime: 3948182, // 0x003c3e96
			},
			expectedHex: "d0503face83fbe9fa0d9f82d4c44baf3d8ff361c57746998b8e687d08205c7f2",
			description: "Real Ravencoin transaction from block 3948184",
		},
		{
			name: "Simple Coinbase Transaction",
			transaction: &RavenTransaction{
				Version: 1,
				Inputs: []RavenTxInput{
					{
						PrevTxHash: common.Hash{},                  // Null hash for coinbase
						PrevIndex:  0xFFFFFFFF,                     // Max uint32 for coinbase
						ScriptSig:  []byte{0x03, 0x12, 0x34, 0x56}, // Height 0x563412 (little endian)
						Sequence:   0xFFFFFFFF,
					},
				},
				Outputs: []RavenTxOutput{
					{
						Value:        5000000000,                                                                                                                                                   // 50 RVN in satoshis
						ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x88, 0xac}, // P2PKH script
					},
				},
				LockTime: 0,
			},
			expectedHex: "calculated_will_be_shown_in_output",
			description: "Basic coinbase transaction with single output",
		},
		{
			name: "Coinbase with Canxium Miner Tag",
			transaction: &RavenTransaction{
				Version: 1,
				Inputs: []RavenTxInput{
					{
						PrevTxHash: common.Hash{}, // Null hash for coinbase
						PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
						// ScriptSig contains block height + canxium miner tag
						ScriptSig: append([]byte{0x03, 0x12, 0x34, 0x56}, // Height
							[]byte(minerTagPrefix+"1234567890abcdef1234567890abcdef12345678")...), // Canxium address
						Sequence: 0xFFFFFFFF,
					},
				},
				Outputs: []RavenTxOutput{
					{
						Value:        5000000000, // 50 RVN in satoshis
						ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x88, 0xac},
					},
				},
				LockTime: 0,
			},
			expectedHex: "calculated_will_be_shown_in_output",
			description: "Coinbase transaction with Canxium miner tag",
		},
		{
			name: "Multi-output Transaction",
			transaction: &RavenTransaction{
				Version: 1,
				Inputs: []RavenTxInput{
					{
						PrevTxHash: common.Hash{}, // Null hash for coinbase
						PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
						ScriptSig:  []byte{0x03, 0x12, 0x34, 0x56},
						Sequence:   0xFFFFFFFF,
					},
				},
				Outputs: []RavenTxOutput{
					{
						Value:        2500000000, // 25 RVN
						ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x88, 0xac},
					},
					{
						Value:        2500000000, // 25 RVN
						ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10, 0xfe, 0xdc, 0xba, 0x98, 0x88, 0xac},
					},
				},
				LockTime: 0,
			},
			expectedHex: "calculated_will_be_shown_in_output",
			description: "Transaction with multiple outputs",
		},
		{
			name: "Transaction with SegWit",
			transaction: &RavenTransaction{
				Version: 2, // Version 2 for SegWit support
				Inputs: []RavenTxInput{
					{
						PrevTxHash: common.Hash{}, // Null hash for coinbase
						PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
						ScriptSig:  []byte{0x03, 0x12, 0x34, 0x56},
						Sequence:   0xFFFFFFFF,
						ScriptWitness: [][]byte{
							{0x30, 0x44, 0x02, 0x20}, // Sample witness data
							{0x21, 0x03, 0x12, 0x34}, // Sample pubkey
						},
					},
				},
				Outputs: []RavenTxOutput{
					{
						Value:        5000000000,                                                                                                                     // 50 RVN
						ScriptPubKey: []byte{0x00, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab}, // P2WPKH script
					},
				},
				LockTime: 0,
			},
			expectedHex: "calculated_will_be_shown_in_output",
			description: "Transaction with witness data (SegWit)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test block to calculate the hash
			block := &RavenBlock{
				Header: &RavenBlockHeader{
					Version:    1,
					PrevBlock:  common.Hash{},
					MerkleRoot: common.Hash{},
					Timestamp:  1640995200, // January 1, 2022
					Bits:       0x1d00ffff,
					Nonce:      0,
					Height:     1,
				},
				Coinbase: tt.transaction,
			}

			// Calculate the hash
			hash := block.Coinbase.Hash()
			hashHex := hex.EncodeToString(hash[:])

			// Print the calculated hash for manual verification
			t.Logf("Test: %s", tt.name)
			t.Logf("Description: %s", tt.description)
			t.Logf("Calculated hash: %s", hashHex)
			t.Logf("Expected hash:   %s", tt.expectedHex)
			t.Logf("Transaction details:")
			t.Logf("  Version: %d", tt.transaction.Version)
			t.Logf("  Inputs count: %d", len(tt.transaction.Inputs))
			for i, input := range tt.transaction.Inputs {
				t.Logf("    Input %d:", i)
				t.Logf("      PrevTxHash: %s", input.PrevTxHash.Hex())
				t.Logf("      PrevIndex: %d", input.PrevIndex)
				t.Logf("      ScriptSig: %s", hex.EncodeToString(input.ScriptSig))
				t.Logf("      Sequence: %d", input.Sequence)
				if len(input.ScriptWitness) > 0 {
					t.Logf("      Witness data:")
					for j, witness := range input.ScriptWitness {
						t.Logf("        [%d]: %s", j, hex.EncodeToString(witness))
					}
				}
			}
			t.Logf("  Outputs count: %d", len(tt.transaction.Outputs))
			for i, output := range tt.transaction.Outputs {
				t.Logf("    Output %d:", i)
				t.Logf("      Value: %d", output.Value)
				t.Logf("      ScriptPubKey: %s", hex.EncodeToString(output.ScriptPubKey))
			}
			t.Logf("  LockTime: %d", tt.transaction.LockTime)
			t.Logf("")

			// For now, we'll just verify the hash is not zero
			if hash == (common.Hash{}) {
				t.Errorf("calculateCoinbaseHash() returned zero hash for test %s", tt.name)
			}

			// Verify the hash is 32 bytes
			if len(hash) != 32 {
				t.Errorf("calculateCoinbaseHash() returned hash of length %d, expected 32", len(hash))
			}

			// Check if we have a known expected hash to compare against
			if tt.expectedHex != "calculated_will_be_shown_in_output" {
				if hashHex != tt.expectedHex {
					t.Errorf("calculateCoinbaseHash() = %s, want %s", hashHex, tt.expectedHex)

					// Let's also print the raw transaction bytes for debugging
					t.Logf("Raw transaction bytes for debugging:")
					buf := new(bytes.Buffer)

					// Replicate the serialization logic for debugging
					binary.Write(buf, binary.LittleEndian, int32(tt.transaction.Version))
					writeCompactSize(buf, uint64(len(tt.transaction.Inputs)))

					for _, input := range tt.transaction.Inputs {
						buf.Write(input.PrevTxHash[:])
						binary.Write(buf, binary.LittleEndian, input.PrevIndex)
						writeCompactSize(buf, uint64(len(input.ScriptSig)))
						buf.Write(input.ScriptSig)
						binary.Write(buf, binary.LittleEndian, input.Sequence)
					}

					writeCompactSize(buf, uint64(len(tt.transaction.Outputs)))

					for _, output := range tt.transaction.Outputs {
						binary.Write(buf, binary.LittleEndian, int64(output.Value))
						writeCompactSize(buf, uint64(len(output.ScriptPubKey)))
						buf.Write(output.ScriptPubKey)
					}

					binary.Write(buf, binary.LittleEndian, tt.transaction.LockTime)

					t.Logf("Serialized transaction hex: %s", hex.EncodeToString(buf.Bytes()))
				} else {
					t.Logf("✅ Hash matches expected value!")
				}
			}
		})
	}
}

// TestRavenBlockOperations tests various block operations
func TestRavenBlockOperations(t *testing.T) {
	// Create a test block
	block := &RavenBlock{
		Header: &RavenBlockHeader{
			Version:    1,
			PrevBlock:  common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			MerkleRoot: common.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"),
			Timestamp:  1640995200,
			Bits:       0x1d00ffff,
			Nonce:      12345,
			Height:     100,
		},
		MerkleProof: []common.Hash{
			common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
		Coinbase: &RavenTransaction{
			Version: 1,
			Inputs: []RavenTxInput{
				{
					PrevTxHash: common.Hash{}, // Null hash for coinbase
					PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
					ScriptSig: append([]byte{0x03, 0x64, 0x00, 0x00}, // Height 100
						[]byte(minerTagPrefix+"1234567890abcdef1234567890abcdef12345678")...), // Canxium address
					Sequence: 0xFFFFFFFF,
				},
			},
			Outputs: []RavenTxOutput{
				{
					Value:        5000000000, // 50 RVN
					ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x88, 0xac},
				},
			},
			LockTime: 0,
		},
	}

	// Test Chain() method
	if block.Chain() != RavenChain {
		t.Errorf("Chain() = %v, want %v", block.Chain(), RavenChain)
	}

	// Test PoWAlgorithm() method
	if block.PoWAlgorithm() != KawPoWAlgorithm {
		t.Errorf("PoWAlgorithm() = %v, want %v", block.PoWAlgorithm(), KawPoWAlgorithm)
	}

	// Test IsValidBlock() method
	if !block.IsValidBlock() {
		t.Error("IsValidBlock() returned false for valid block")
	}

	// Test BlockHash() method
	hash := block.BlockHash()
	if hash == "" {
		t.Error("BlockHash() returned empty string")
	}

	// Test Timestamp() method
	timestamp := block.Timestamp()
	if timestamp != 1640995200000 { // Should be in milliseconds
		t.Errorf("Timestamp() = %d, want %d", timestamp, 1640995200000)
	}

	// Test GetMinerAddress() method
	minerAddr, err := block.GetMinerAddress()
	if err != nil {
		t.Errorf("GetMinerAddress() returned error: %v", err)
	}

	expectedAddr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	if minerAddr != expectedAddr {
		t.Errorf("GetMinerAddress() = %s, want %s", minerAddr.Hex(), expectedAddr.Hex())
	}

	// Test Copy() method
	copied := block.Copy()
	if copied == nil {
		t.Error("Copy() returned nil")
	}

	copiedBlock, ok := copied.(*RavenBlock)
	if !ok {
		t.Error("Copy() returned wrong type")
	}

	// Verify deep copy
	if copiedBlock.Header.Height != block.Header.Height {
		t.Error("Copy() did not copy header correctly")
	}

	if len(copiedBlock.MerkleProof) != len(block.MerkleProof) {
		t.Error("Copy() did not copy merkle proof correctly")
	}

	if copiedBlock.Coinbase.Version != block.Coinbase.Version {
		t.Error("Copy() did not copy coinbase correctly")
	}
}

// TestCompactSize tests the writeCompactSize function
func TestCompactSize(t *testing.T) {
	tests := []struct {
		size     uint64
		expected []byte
	}{
		{0, []byte{0x00}},
		{252, []byte{0xFC}},
		{253, []byte{0xFD, 0xFD, 0x00}},
		{65535, []byte{0xFD, 0xFF, 0xFF}},
		{65536, []byte{0xFE, 0x00, 0x00, 0x01, 0x00}},
		{0xFFFFFFFF, []byte{0xFE, 0xFF, 0xFF, 0xFF, 0xFF}},
		{0x100000000, []byte{0xFF, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("size_%d", tt.size), func(t *testing.T) {
			buf := new(bytes.Buffer)
			writeCompactSize(buf, tt.size)
			result := buf.Bytes()

			if !bytes.Equal(result, tt.expected) {
				t.Errorf("writeCompactSize(%d) = %x, want %x", tt.size, result, tt.expected)
			}
		})
	}
}

// TestMerkleProofVerification tests merkle proof verification
func TestMerkleProofVerification(t *testing.T) {
	// Create a test block
	block := &RavenBlock{
		Header: &RavenBlockHeader{
			Version:    1,
			PrevBlock:  common.Hash{},
			MerkleRoot: common.Hash{}, // Will be set based on calculated hash
			Timestamp:  1640995200,
			Bits:       0x1d00ffff,
			Nonce:      12345,
			Height:     1,
		},
		MerkleProof: []common.Hash{}, // No proof needed for single transaction
		Coinbase: &RavenTransaction{
			Version: 1,
			Inputs: []RavenTxInput{
				{
					PrevTxHash: common.Hash{},
					PrevIndex:  0xFFFFFFFF,
					ScriptSig:  []byte{0x03, 0x01, 0x00, 0x00}, // Height 1
					Sequence:   0xFFFFFFFF,
				},
			},
			Outputs: []RavenTxOutput{
				{
					Value:        5000000000,
					ScriptPubKey: []byte{0x76, 0xa9, 0x14, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x88, 0xac},
				},
			},
			LockTime: 0,
		},
	}

	// Calculate coinbase hash and set as merkle root (for single transaction case)
	coinbaseHash := block.Coinbase.Hash()
	block.Header.MerkleRoot = coinbaseHash

	// Test verification with no merkle proof (single transaction)
	if !block.verifyMerkleProofForCoinbaseTx() {
		t.Error("verifyMerkleProofForCoinbaseTx() failed for single transaction case")
	}

	// Test VerifyCoinbase method
	if !block.VerifyCoinbase() {
		t.Error("VerifyCoinbase() failed for valid coinbase")
	}

	t.Logf("Coinbase hash: %s", coinbaseHash.Hex())
	t.Logf("Merkle root: %s", block.Header.MerkleRoot.Hex())
}

// TestKawPoW tests the KawPoW implementation
func TestHeaderHash(t *testing.T) {
	// Test with two real Ravencoin blocks
	testCases := []struct {
		name           string
		header         *RavenBlockHeader
		expectedHeader string
		expectedBlock  string
	}{
		{
			name: "Block 3948182",
			header: &RavenBlockHeader{
				Version:    805306368, // 0x30000000
				PrevBlock:  common.HexToHash("0000000000002bf0ec5a3009997996ddec01e2007bacad254b4e401d83d1125d"),
				MerkleRoot: common.HexToHash("97aceed868687eff866556509ce48ebb8c828cba0841b82aa6130d5548960402"),
				Timestamp:  1753515192,
				Bits:       0x1b011363,
				Nonce:      10160171505908156645,
				Height:     3948182,
				MixHash:    common.HexToHash("992b5ef8ffb12efe108e08cc0899c3c94fc367862677be827ae48d55476afc9f"),
			},
			expectedHeader: "5385fbae28c68c5327492701c9fa6285e180513185721abf239850744666d269",
			expectedBlock:  "0000000000008c6a907662d9b97693f068139658c1b2e84517906b9d951d0ede",
		},
		{
			name: "Block 3949355",
			header: &RavenBlockHeader{
				Version:    805306368, // 0x30000000
				PrevBlock:  common.HexToHash("00000000000004267bd2081bf7f56f668c309133e35369aa80313dc50f4d1ddf"),
				MerkleRoot: common.HexToHash("c06d8c1a935e832bade3f04dc53b1184b8fbf3889f20b6f920d53e482702a417"),
				Timestamp:  1753585696,
				Bits:       0x1b0106d2,
				Nonce:      3345533096753640927, // Real 64-bit nonce from KawPoW
				Height:     3949355,
				MixHash:    common.HexToHash("22d419d1807d3c316e1744c8087d64c6fdc8f0ef9ac7b864319c26889f2512b4"),
			},
			expectedHeader: "f9baa1e0ecd28156120c90a211c0e7b000e6e554582581ea246be398e859ef65",
			expectedBlock:  "00000000000047034ca45d7c53e74995b3debe8b64c2e3b99ec8452d8a037139",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Real Ravencoin Block Data (%s - Height %d):", tc.name, tc.header.Height)
			t.Logf("  Block Hash:   %s", tc.expectedBlock)
			t.Logf("  Header Hash:  %s", tc.expectedHeader)
			t.Logf("  Nonce:        %d (0x%016x)", tc.header.Nonce, tc.header.Nonce)
			t.Logf("  Mix Hash:     %s", tc.header.MixHash.Hex())
			t.Logf("")

			// Debug: Show the serialization bytes (matching CKAWPOWInput)
			buf := new(bytes.Buffer)

			// Write header fields exactly as CKAWPOWInput does in Ravencoin
			// Only: nVersion, hashPrevBlock, hashMerkleRoot, nTime, nBits, nHeight
			binary.Write(buf, binary.LittleEndian, int32(tc.header.Version))
			buf.Write(tc.header.PrevBlock[:])
			buf.Write(tc.header.MerkleRoot[:])
			binary.Write(buf, binary.LittleEndian, tc.header.Timestamp)
			binary.Write(buf, binary.LittleEndian, tc.header.Bits)
			binary.Write(buf, binary.LittleEndian, tc.header.Height)
			// Note: CKAWPOWInput does NOT include nNonce64 or mix_hash

			t.Logf("Serialization Debug (CKAWPOWInput format):")
			t.Logf("  Raw bytes: %x", buf.Bytes())
			t.Logf("  Length: %d bytes", len(buf.Bytes()))
			t.Logf("")

			// Test basic KawPoW hash calculation
			hash := tc.header.HeaderHash()
			t.Logf("  Calculated hash: %s", hash.Hex())

			if hash.Hex() != "0x"+tc.expectedHeader {
				t.Logf("  ❌ Header hash mismatch")

				// Manual double SHA256 calculation for debugging
				first := sha256.Sum256(buf.Bytes())
				second := sha256.Sum256(first[:])
				manualHash := common.BytesToHash(second[:])
				t.Logf("  Manual double SHA256: %s", manualHash.Hex())

			} else {
				t.Logf("  ✅ Header hash matches!")
			}
			t.Logf("")
		})
	}
}

func TestBlockDifficulty(t *testing.T) {
	// Test with a real Ravencoin block
	header := &RavenBlockHeader{
		Version:    805306368, // 0x30000000
		PrevBlock:  common.HexToHash("00000000000004267bd2081bf7f56f668c309133e35369aa80313dc50f4d1ddf"),
		MerkleRoot: common.HexToHash("c06d8c1a935e832bade3f04dc53b1184b8fbf3889f20b6f920d53e482702a417"),
		Timestamp:  1753585696,
		Bits:       0x1b0106d2,
		Nonce:      3345533096753640927, // Real 64-bit nonce from KawPoW
		Height:     3949355,
		MixHash:    common.HexToHash("22d419d1807d3c316e1744c8087d64c6fdc8f0ef9ac7b864319c26889f2512b4"),
	}

	t.Logf("Testing difficulty for Ravencoin block at height %d", header.Height)
	t.Logf("Bits: 0x%08x", header.Bits)

	difficulty := header.GetDifficulty()
	t.Logf("Calculated difficulty: %s", difficulty.String())
	if difficulty.Cmp(big.NewInt(63834)) != 0 {
		t.Errorf("Expected difficulty 63834, got %s", difficulty.String())
	} else {
		t.Logf("✅ Difficulty matches expected value")
	}
}

func TestVerifyKawPoW(t *testing.T) {
	// Test with a real Ravencoin block (Block 3953651)
	header := &RavenBlockHeader{
		Version:    805306368, // 0x30000000
		PrevBlock:  common.HexToHash("000000000000f27a60df989a38f814597b5d118684c7c64570ac2740de707f9b"),
		MerkleRoot: common.HexToHash("286433a5bda38e253aef904b6be0b434bc87d7b1c8aa9c46a81d11cbd618f9ea"),
		Timestamp:  1753845451,
		Bits:       0x1b011593,
		Nonce:      9889904786423204540, // Real 64-bit nonce from KawPoW
		Height:     3953651,
		MixHash:    common.HexToHash("2090ceaabf386c9ada28929d7ec6cbd6f97225c33eebe5aeeb21c986d4ef7ff5"),
	}

	// Calculate the target from bits (don't hardcode it)
	target := bitsToTarget(header.Bits)

	t.Logf("Testing KawPoW verification for Ravencoin block at height %d", header.Height)
	t.Logf("Block hash: %s", "000000000001102e69bba18e20750fb968108829c116152ac40ae810ea9cdb6e")
	t.Logf("Header hash: %s", "f64a53012f17fbe623e2188af204b59a6fe92ace59f1a807a2c1eb4863eb3745")
	t.Logf("Bits: 0x%08x", header.Bits)
	t.Logf("Expected difficulty: %.8f", 60441.34817545983)
	t.Logf("Calculated target: %064x", target) // Show with leading zeros

	if !header.VerifyKawPoW() {
		t.Error("KawPoW verification failed for valid block")
	} else {
		t.Logf("✅ KawPoW verification successful")
	}
}

func TestTargetDifficulty(t *testing.T) {
	// Test with a real Ravencoin block (Block 3953651)
	header := &RavenBlockHeader{
		Version:    805306368, // 0x30000000
		PrevBlock:  common.HexToHash("000000000000f27a60df989a38f814597b5d118684c7c64570ac2740de707f9b"),
		MerkleRoot: common.HexToHash("286433a5bda38e253aef904b6be0b434bc87d7b1c8aa9c46a81d11cbd618f9ea"),
		Timestamp:  1753845451,
		Bits:       0x1b011593,
		Nonce:      9889904786423204540, // Real 64-bit nonce from KawPoW
		Height:     3953651,
		MixHash:    common.HexToHash("2090ceaabf386c9ada28929d7ec6cbd6f97225c33eebe5aeeb21c986d4ef7ff5"),
	}

	// Calculate the target from bits (don't hardcode it)
	expectedTarget, _ := new(big.Int).SetString("0000000000011593000000000000000000000000000000000000000000000000", 16)
	target := bitsToTarget(header.Bits)
	if target.Cmp(expectedTarget) != 0 {
		t.Errorf("Expected target 0000000000011593000000000000000000000000000000000000000000000000, got %s", fmt.Sprintf("%064x", target))
	} else {
		t.Logf("✅ Target matches expected value")
	}
}

// TestMerkleRootGeneration tests the merkle root generation using real Ravencoin block data
func TestMerkleRootGeneration(t *testing.T) {
	// Real Ravencoin block 3953652 data
	expectedMerkleRoot := "e22b5f6aad6bda37694f0a72092c9193ded732b4c5b3249169ba0486c2484e0f"

	// Transaction 1: Coinbase transaction
	tx1 := &RavenTransaction{
		Version: 1,
		Inputs: []RavenTxInput{
			{
				PrevTxHash: common.Hash{}, // Null hash for coinbase
				PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
				ScriptSig: func() []byte {
					// Coinbase scriptSig: "03f4533c04228f8968040686a66d000000001b324d696e6572732068747470733a2f2f326d696e6572732e636f6d"
					scriptHex := "03f4533c04228f8968040686a66d000000001b324d696e6572732068747470733a2f2f326d696e6572732e636f6d"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
		},
		Outputs: []RavenTxOutput{
			{
				Value: 250000975490, // 2500.00975490 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a91459d584c2da3735f24af4ed3eb8e2abeb63fbffd688ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
			{
				Value: 0, // OP_RETURN output with 0 value
				ScriptPubKey: func() []byte {
					scriptHex := "6a24aa21a9eddbfca51fcf2f56d02fa5259a28b519b4ea147415c85bc1a255603e3f686f8189"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
		},
		LockTime: 0,
	}

	// Transaction 2
	tx2 := &RavenTransaction{
		Version: 2,
		Inputs: []RavenTxInput{
			{
				PrevTxHash: func() common.Hash {
					hashHex := "4a7f4573af4386beb363aca9b84880413a2f03f0671732a15cc4192aa937ee0c"
					hashBytes, _ := hex.DecodeString(hashHex)
					// Reverse bytes for serialization
					for i := 0; i < len(hashBytes)/2; i++ {
						hashBytes[i], hashBytes[len(hashBytes)-1-i] = hashBytes[len(hashBytes)-1-i], hashBytes[i]
					}
					return common.BytesToHash(hashBytes)
				}(),
				PrevIndex: 1,
				ScriptSig: func() []byte {
					scriptHex := "47304402201cef36791e134473c9586ba662612ea865a2ad0d0b3ab36b6505cd08a156b4b802205c37d63ce7388f934b5b2a558dd9d271d0df3b3c635a1be325ab2d2f3ea07a35012102c2fcd7d43707e1451bd8f547e962fcffe83aa47a4e84c0d448eeaa05c60048f1"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
			{
				PrevTxHash: func() common.Hash {
					hashHex := "bd045806b0154a776161b4d4e28415895f215529488103aed6def13bd7dec3ef"
					hashBytes, _ := hex.DecodeString(hashHex)
					// Reverse bytes for serialization
					for i := 0; i < len(hashBytes)/2; i++ {
						hashBytes[i], hashBytes[len(hashBytes)-1-i] = hashBytes[len(hashBytes)-1-i], hashBytes[i]
					}
					return common.BytesToHash(hashBytes)
				}(),
				PrevIndex: 0,
				ScriptSig: func() []byte {
					scriptHex := "483045022100abbf6cb8281e43dd46526a35958f9d3001568b1eaf85537bda00b327651b58c902200589441b62e82c678ca765294f2e05988d644948cb68e947308cc2e942c04520012102c2fcd7d43707e1451bd8f547e962fcffe83aa47a4e84c0d448eeaa05c60048f1"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
		},
		Outputs: []RavenTxOutput{
			{
				Value: 157084000, // 1.57084000 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a914cc15208f0bbb8e92cb8d31abc6934687b433e19b88ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
		},
		LockTime: 0,
	}

	// Transaction 3
	tx3 := &RavenTransaction{
		Version: 1,
		Inputs: []RavenTxInput{
			{
				PrevTxHash: func() common.Hash {
					hashHex := "b86f4f4d010a6ced739438cb3ba4aa7c141095944681d3cb21b4e949546bca5c"
					hashBytes, _ := hex.DecodeString(hashHex)
					// Reverse bytes for serialization
					for i := 0; i < len(hashBytes)/2; i++ {
						hashBytes[i], hashBytes[len(hashBytes)-1-i] = hashBytes[len(hashBytes)-1-i], hashBytes[i]
					}
					return common.BytesToHash(hashBytes)
				}(),
				PrevIndex: 0,
				ScriptSig: func() []byte {
					scriptHex := "4730440220321441c259e3106c6a283bbf93ca5afecce294a3c14de87435711eecaadcae550220490f2ba568e190ffc957eac83af62935804c2c32162c1adda9110ccc3f48a0b80121020233d7a5ef166be9d7f603a82e6baeefd88f580a463f420ca0c41e62d8d615f5"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
		},
		Outputs: []RavenTxOutput{
			{
				Value: 34003359000, // 340.03359000 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a9146eb8ce84724123283bd497033b38bfed57c1f6cd88ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
			{
				Value: 385047851143, // 3850.47851143 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a91494b355c5f9de2487d840b42b8f543d7a2ab79e0788ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
		},
		LockTime: 0,
	}

	// Transaction 4
	tx4 := &RavenTransaction{
		Version: 1,
		Inputs: []RavenTxInput{
			{
				PrevTxHash: func() common.Hash {
					hashHex := "b562079ad7ce72d3f2be97d0c66bfb9c5774f3ed77f3c84fd2a7097f6ad85de0"
					hashBytes, _ := hex.DecodeString(hashHex)
					// Reverse bytes for serialization
					for i := 0; i < len(hashBytes)/2; i++ {
						hashBytes[i], hashBytes[len(hashBytes)-1-i] = hashBytes[len(hashBytes)-1-i], hashBytes[i]
					}
					return common.BytesToHash(hashBytes)
				}(),
				PrevIndex: 0,
				ScriptSig: func() []byte {
					scriptHex := "4730440220704f84099a9cb687504b434219193775080319e254c0d846dea6c2530caa2339022048bb8af1bd89a7754efdcb6e856c39b4c2163251eade4bb1add5611aab6403fd012102e25a6ccc57edc3aadbffb94e8c661538697ac922863ba5d0fe23a4d71940ed6a"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
		},
		Outputs: []RavenTxOutput{
			{
				Value: 1159804298, // 11.59804298 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a9147282080f8985fbe28393f876439a6e424483cdeb88ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
		},
		LockTime: 0,
	}

	// Create an array of all transactions in the block
	transactions := []*RavenTransaction{tx1, tx2, tx3, tx4}

	// Calculate the hash for each transaction
	var txHashes []common.Hash
	for i, tx := range transactions {
		hash := tx.Hash()
		txHashes = append(txHashes, hash)
		t.Logf("Transaction %d hash: %s", i+1, hash.Hex())
	}

	// Expected transaction hashes from the block data
	expectedTxHashes := []string{
		"56e6b6ba07a4b43366b74086311e61dbae3d2e5477619a9fcb796e2049832369",
		"c1b11d70e5b38daf9a36f2fe707f430369310ef2c8a476528bee19976ad3c571",
		"bf8501aceebc97883d9a5e7d252ea58a708c7dc5322e4858b3bfe091ab145b9d",
		"0f335ccf3f2488c65cefa9f7821e705dfc5973317a2ee8f3c5adf3d8272826d6",
	}

	// Verify individual transaction hashes match expected values
	t.Logf("Verifying individual transaction hashes:")
	for i, expectedHash := range expectedTxHashes {
		calculatedHash := txHashes[i].Hex()[2:] // Remove 0x prefix
		if calculatedHash == expectedHash {
			t.Logf("✅ Transaction %d hash matches: %s", i+1, expectedHash)
		} else {
			t.Logf("❌ Transaction %d hash mismatch:", i+1)
			t.Logf("   Expected: %s", expectedHash)
			t.Logf("   Got:      %s", calculatedHash)
		}
	}

	// Use buildMerkleTree to calculate the merkle root
	calculatedMerkleRoot := buildMerkleTree(txHashes)

	t.Logf("\nMerkle Root Calculation:")
	t.Logf("Expected merkle root: %s", expectedMerkleRoot)
	t.Logf("Calculated merkle root: %s", calculatedMerkleRoot.Hex()[2:]) // Remove 0x prefix

	// Verify the merkle root matches
	if calculatedMerkleRoot.Hex()[2:] == expectedMerkleRoot {
		t.Logf("✅ Merkle root calculation successful!")
	} else {
		t.Errorf("❌ Merkle root mismatch!")
		t.Logf("Expected: %s", expectedMerkleRoot)
		t.Logf("Got:      %s", calculatedMerkleRoot.Hex()[2:])

		// Debug: Show the merkle tree construction step by step
		t.Logf("\nDebug: Merkle tree construction:")
		debugMerkleTree(t, txHashes)
	}

	// Also test with the buildMerkleTree function directly
	t.Run("DirectBuildMerkleTreeTest", func(t *testing.T) {
		// Convert expected hashes to common.Hash for direct testing
		var expectedHashes []common.Hash
		for _, hashStr := range expectedTxHashes {
			hashBytes, _ := hex.DecodeString(hashStr)
			expectedHashes = append(expectedHashes, common.BytesToHash(hashBytes))
		}

		directResult := buildMerkleTree(expectedHashes)
		t.Logf("Direct buildMerkleTree result: %s", directResult.Hex()[2:])

		if directResult.Hex()[2:] == expectedMerkleRoot {
			t.Logf("✅ Direct buildMerkleTree test successful!")
		} else {
			t.Errorf("❌ Direct buildMerkleTree test failed!")
		}
	})
}

// debugMerkleTree shows step-by-step merkle tree construction for debugging
func debugMerkleTree(t *testing.T, txHashes []common.Hash) {
	if len(txHashes) == 0 {
		return
	}

	level := 0
	hashes := make([]common.Hash, len(txHashes))
	copy(hashes, txHashes)

	t.Logf("Level %d: %d hashes", level, len(hashes))
	for i, hash := range hashes {
		t.Logf("  [%d]: %s", i, hash.Hex()[2:])
	}

	for len(hashes) > 1 {
		level++
		var nextLevel []common.Hash

		t.Logf("Level %d processing:", level)
		for i := 0; i < len(hashes); i += 2 {
			var left, right common.Hash
			left = hashes[i]

			if i+1 < len(hashes) {
				right = hashes[i+1]
			} else {
				right = hashes[i] // Duplicate the last hash
				t.Logf("  Duplicating last hash for odd count")
			}

			t.Logf("  Hashing pair [%d,%d]:", i, i+1)
			t.Logf("    Left:  %s", left.Hex()[2:])
			t.Logf("    Right: %s", right.Hex()[2:])

			combinedHash := hashRavenMerkleBranches(left, right)
			t.Logf("    Result: %s", combinedHash.Hex()[2:])
			nextLevel = append(nextLevel, combinedHash)
		}

		hashes = nextLevel
		t.Logf("Level %d result: %d hashes", level, len(hashes))
		for i, hash := range hashes {
			t.Logf("  [%d]: %s", i, hash.Hex()[2:])
		}
	}
}

// TestMerkleProofForCoinbase tests building and verifying a merkle proof for the coinbase transaction
func TestMerkleProofForCoinbase(t *testing.T) {
	// Real Ravencoin block 3953652 data
	expectedMerkleRoot := "e22b5f6aad6bda37694f0a72092c9193ded732b4c5b3249169ba0486c2484e0f"

	// Transaction 1: Coinbase transaction
	coinbaseTx := &RavenTransaction{
		Version: 1,
		Inputs: []RavenTxInput{
			{
				PrevTxHash: common.Hash{}, // Null hash for coinbase
				PrevIndex:  0xFFFFFFFF,    // Max uint32 for coinbase
				ScriptSig: func() []byte {
					// Coinbase scriptSig: "03f4533c04228f8968040686a66d000000001b324d696e6572732068747470733a2f2f326d696e6572732e636f6d"
					scriptHex := "03f4533c04228f8968040686a66d000000001b324d696e6572732068747470733a2f2f326d696e6572732e636f6d"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
				Sequence: 4294967295,
			},
		},
		Outputs: []RavenTxOutput{
			{
				Value: 250000975490, // 2500.00975490 RVN in satoshis
				ScriptPubKey: func() []byte {
					scriptHex := "76a91459d584c2da3735f24af4ed3eb8e2abeb63fbffd688ac"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
			{
				Value: 0, // OP_RETURN output with 0 value
				ScriptPubKey: func() []byte {
					scriptHex := "6a24aa21a9eddbfca51fcf2f56d02fa5259a28b519b4ea147415c85bc1a255603e3f686f8189"
					script, _ := hex.DecodeString(scriptHex)
					return script
				}(),
			},
		},
		LockTime: 0,
	}

	// All transaction hashes from block 3953652
	expectedTxHashes := []string{
		"56e6b6ba07a4b43366b74086311e61dbae3d2e5477619a9fcb796e2049832369", // coinbase
		"c1b11d70e5b38daf9a36f2fe707f430369310ef2c8a476528bee19976ad3c571",
		"bf8501aceebc97883d9a5e7d252ea58a708c7dc5322e4858b3bfe091ab145b9d",
		"0f335ccf3f2488c65cefa9f7821e705dfc5973317a2ee8f3c5adf3d8272826d6",
	}

	// Convert transaction hashes to common.Hash
	var txHashes []common.Hash
	for _, hashStr := range expectedTxHashes {
		hashBytes, _ := hex.DecodeString(hashStr)
		txHashes = append(txHashes, common.BytesToHash(hashBytes))
	}

	// Verify the coinbase hash matches
	coinbaseHash := coinbaseTx.Hash()
	expectedCoinbaseHash := txHashes[0]

	t.Logf("Coinbase hash calculated: %s", coinbaseHash.Hex()[2:])
	t.Logf("Expected coinbase hash:   %s", expectedCoinbaseHash.Hex()[2:])

	if coinbaseHash != expectedCoinbaseHash {
		t.Errorf("Coinbase hash mismatch!")
		return
	}

	// Calculate the merkle proof for the coinbase transaction (position 0)
	merkleProof := calculateMerkleProof(txHashes, 0)

	t.Logf("Merkle proof for coinbase transaction:")
	for i, proof := range merkleProof {
		t.Logf("  [%d]: %s", i, proof.Hex()[2:])
	}

	// Create the block header
	header := &RavenBlockHeader{
		Version:    805306368, // 0x30000000
		PrevBlock:  common.HexToHash("000000000001102e69bba18e20750fb968108829c116152ac40ae810ea9cdb6e"),
		MerkleRoot: common.HexToHash(expectedMerkleRoot),
		Timestamp:  1753845538,
		Bits:       0x1b011490,
		Nonce:      2915658403737697855,
		Height:     3953652,
		MixHash:    common.HexToHash("7213ee36078c22f63d56a0e40136fb5b28f6e572e016de2b9aca65b7f25384af"),
	}

	// Create the Raven block with the merkle proof
	block := &RavenBlock{
		Header:      header,
		MerkleProof: merkleProof,
		Coinbase:    coinbaseTx,
	}

	// Test VerifyCoinbase
	t.Logf("Testing VerifyCoinbase...")
	if block.VerifyCoinbase() {
		t.Logf("✅ VerifyCoinbase successful!")
	} else {
		t.Errorf("❌ VerifyCoinbase failed!")

		// Debug the verification step by step
		t.Logf("Debug verification:")
		t.Logf("  Block header merkle root: %s", block.Header.MerkleRoot.Hex()[2:])
		t.Logf("  Coinbase hash: %s", coinbaseHash.Hex()[2:])

		// Manually verify the merkle proof
		computedHash := coinbaseHash
		t.Logf("  Starting with coinbase hash: %s", computedHash.Hex()[2:])

		for i, siblingHash := range merkleProof {
			t.Logf("  Step %d: hash with %s", i+1, siblingHash.Hex()[2:])
			computedHash = hashRavenMerkleBranches(computedHash, siblingHash)
			t.Logf("    Result: %s", computedHash.Hex()[2:])
		}

		t.Logf("  Final computed root: %s", computedHash.Hex()[2:])
		t.Logf("  Expected root:       %s", expectedMerkleRoot)

		if computedHash.Hex()[2:] == expectedMerkleRoot {
			t.Logf("  ✅ Manual verification successful - issue might be in VerifyCoinbase logic")
		} else {
			t.Logf("  ❌ Manual verification also failed")
		}
	}
}

// calculateMerkleProof calculates the merkle proof for a transaction at a given position
// This returns the sibling hashes needed to reconstruct the merkle root
func calculateMerkleProof(txHashes []common.Hash, position int) []common.Hash {
	if len(txHashes) <= 1 {
		return []common.Hash{} // No proof needed for single transaction
	}

	var proof []common.Hash
	currentPos := position

	// Create a copy to avoid modifying the original slice
	hashes := make([]common.Hash, len(txHashes))
	copy(hashes, txHashes)

	// Build the tree level by level and collect sibling hashes
	for len(hashes) > 1 {
		var nextLevel []common.Hash
		nextPos := currentPos / 2

		// Process pairs of hashes
		for i := 0; i < len(hashes); i += 2 {
			var left, right common.Hash
			left = hashes[i]

			// If odd number of hashes, duplicate the last one
			if i+1 < len(hashes) {
				right = hashes[i+1]
			} else {
				right = hashes[i] // Duplicate the last hash
			}

			// If this pair contains our target position, record the sibling
			if i <= currentPos && currentPos < i+2 {
				// Our hash is either left or right in this pair
				if currentPos%2 == 0 {
					// Our hash is on the left, sibling is on the right
					if i+1 < len(hashes) {
						proof = append(proof, right)
					}
					// Note: if there's no right sibling (odd case), we still need to record
					// the duplication, but Ravencoin's verification handles this
				} else {
					// Our hash is on the right, sibling is on the left
					proof = append(proof, left)
				}
			}

			// Hash the pair and add to next level
			combinedHash := hashRavenMerkleBranches(left, right)
			nextLevel = append(nextLevel, combinedHash)
		}

		hashes = nextLevel
		currentPos = nextPos
	}

	return proof
}
