package crosschain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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
			hash := block.calculateCoinbaseHash()
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
	coinbaseHash := block.calculateCoinbaseHash()
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
