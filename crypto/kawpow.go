package crypto

import (
	"github.com/ethereum/go-ethereum/crypto/kawpow"
)

// KawpowHash generates a KawPoW hash
func KawpowHash(headerHash, nonce string, height int64) ([]byte, []byte) {
	return kawpow.Hash(headerHash, nonce, height)
}
