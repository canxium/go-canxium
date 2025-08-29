//go:build !kawpow
// +build !kawpow

package kawpow

import "C"

import (
	"sync"
)

var mu sync.Mutex

func Hash(headerHash, nonce string, height int64) ([]byte, []byte) {
	panic("kawpow not supported")
}
