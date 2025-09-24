package kawpow

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestKawPoW(t *testing.T) {
	// Create a real ethash instance (not test mode)
	config := Config{
		CacheDir:     "",
		CachesInMem:  1,
		CachesOnDisk: 0,
		PowMode:      ModeNormal, // Use real mode, not test mode
	}
	ethash := New(config, nil, false)
	defer ethash.Close()

	t.Run("BasicFunctionality", func(t *testing.T) {
		// Standard test vectors for Ethash
		blockNumber := uint64(30000)
		headerHash := common.HexToHash("0xffeeddccbbaa9988776655443322110000112233445566778899aabbccddeeff")
		nonce := uint64(0x123456789abcdef0)

		// Expected results for real ethash (not test mode)
		expectedMixHash := common.HexToHash("0x177b565752a375501e11b6d9d3679c2df6197b2cab3a1ba2d6b10b8c71a3d459")
		expectedPowHash := common.HexToHash("0xc824bee0418e3cfb7fae56e0d5b3b8b14ba895777feea81c70c0ba947146da69")

		t.Logf("=== KawPow LIGHT TEST ===")
		t.Logf("Block Number: %d", blockNumber)
		t.Logf("Header Hash: %s", headerHash.Hex())
		t.Logf("Nonce: 0x%x (%d)", nonce, nonce)
		t.Logf("")

		// Call HashimotoLight
		mixHash, finalHash := ethash.KawPowHash(blockNumber, headerHash, nonce)

		t.Logf("Results:")
		t.Logf("  Mix Hash:   %s", mixHash.Hex())
		t.Logf("  Final Hash: %s", finalHash.Hex())
		t.Logf("")

		// Verify non-zero results
		if mixHash == (common.Hash{}) {
			t.Error("Mix hash should not be zero")
		}
		if finalHash == (common.Hash{}) {
			t.Error("Final hash should not be zero")
		}

		if mixHash != expectedMixHash {
			t.Errorf("Mix hash mismatch:\nExpected: %s\nGot:      %s", expectedMixHash.Hex(), mixHash.Hex())
		} else {
			t.Logf("✅ Mix hash test passed")
		}
		if finalHash != expectedPowHash {
			t.Errorf("Final hash mismatch:\nExpected: %s\nGot:      %s", expectedPowHash.Hex(), finalHash.Hex())
		} else {
			t.Logf("✅ Final hash test passed")
		}

		if mixHash == expectedMixHash && finalHash == expectedPowHash {
			t.Logf("✅ Deterministic test passed")
		}
	})
}
