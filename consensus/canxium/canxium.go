// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package ethash implements the ethash proof-of-work consensus engine.
package canxium

import (
	"errors"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rpc"
)

var ErrInvalidDumpMagic = errors.New("invalid dump magic")

var (
	// two256 is a big integer representing 2^256
	two256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

	// sharedEthash is a full instance that can be shared between multiple users.
	sharedEthash *Canxium

	// algorithmRevision is the data structure version used for file naming.
	algorithmRevision = 23

	// dumpMagic is a dataset dump header to sanity check a data dump.
	dumpMagic = []uint32{0xbaddcafe, 0xfee1dead}
)

func init() {
	sharedConfig := Config{
		PowMode:       ModeNormal,
		CachesInMem:   3,
		DatasetsInMem: 1,
	}
	sharedEthash = New(sharedConfig, nil, false)
}

// dataset wraps an ethash dataset with some metadata to allow easier concurrent use.
type dataset struct {
	dataset []uint32 // The actual cache data content
}

// Mode defines the type and amount of PoW verification an ethash engine makes.
type Mode uint

const (
	ModeNormal Mode = iota
	ModeShared
	ModeTest
	ModeFake
	ModeFullFake
)

// Config are the configuration parameters of the ethash.
type Config struct {
	// some cfg are same as ethash to reuse ethash concensus
	CacheDir         string
	CachesInMem      int
	CachesOnDisk     int
	CachesLockMmap   bool
	DatasetDir       string
	DatasetsInMem    int
	DatasetsOnDisk   int
	DatasetsLockMmap bool
	PowMode          Mode

	// When set, notifications sent by the remote sealer will
	// be block header JSON objects instead of work package arrays.
	NotifyFull bool

	Difficulty *big.Int `toml:",omitempty"` // Offline mining difficulty set by the miner
	Algorithm  uint8    `toml:",omitempty"` // Offline mining algorithm set by the miner

	Log log.Logger `toml:"-"`
}

// Canxium is a consensus engine based on proof-of-work implementing the ethash and other
// algorithms
type Canxium struct {
	config Config

	ehash *ethash.Ethash

	// Mining related fields
	rand     *rand.Rand    // Properly seeded random source for nonces
	threads  int           // Number of threads to mine on if mining
	update   chan struct{} // Notification channel to update mining parameters
	hashrate metrics.Meter // Meter tracking the average hashrate
	remote   *remoteSealer

	// The fields below are hooks for testing
	shared    *Canxium      // Shared PoW verifier to avoid cache regeneration
	fakeFail  uint64        // Block number which fails PoW check even in fake mode
	fakeDelay time.Duration // Time delay to sleep for before returning from verify

	lock      sync.Mutex // Ensures thread safety for the in-memory caches and mining fields
	closeOnce sync.Once  // Ensures exit channel will not be closed twice.

	signer common.Address // Ethereum address of the signing key
	signFn SignerFn       // Signer function to authorize hashes with

	// dataset, because transaction mining have no block number, we're using zero as block number
	dataset []uint32
}

// New creates a full sized ethash PoW scheme and starts a background thread for
// remote mining, also optionally notifying a batch of remote services of new work
// packages.
func New(config Config, notify []string, noverify bool) *Canxium {
	if config.Log == nil {
		config.Log = log.Root()
	}

	canxium := &Canxium{
		config:   config,
		update:   make(chan struct{}),
		hashrate: metrics.NewMeterForced(),
	}
	if config.PowMode == ModeShared {
		canxium.shared = sharedEthash
	}
	if config.Algorithm == types.EthashAlgorithm {
		canxium.ehash = ethash.New(ethash.Config{
			PowMode:          ethash.Mode(config.PowMode),
			CacheDir:         config.CacheDir,
			CachesInMem:      config.CachesInMem,
			CachesOnDisk:     config.CachesOnDisk,
			CachesLockMmap:   config.CachesLockMmap,
			DatasetDir:       config.DatasetDir,
			DatasetsInMem:    config.DatasetsInMem,
			DatasetsOnDisk:   config.DatasetsOnDisk,
			DatasetsLockMmap: config.DatasetsLockMmap,
			NotifyFull:       config.NotifyFull,
		}, notify, noverify)

		canxium.dataset = canxium.ehash.Dataset(0, false).Dataset()
	}
	canxium.remote = startRemoteSealer(canxium, notify, noverify)
	return canxium
}

// NewTester creates a small sized ethash PoW scheme useful only for testing
// purposes.
func NewTester(notify []string, noverify bool) *Canxium {
	return New(Config{PowMode: ModeTest}, notify, noverify)
}

// NewFaker creates a ethash consensus engine with a fake PoW scheme that accepts
// all blocks' seal as valid, though they still have to conform to the Ethereum
// consensus rules.
func NewFaker() *Canxium {
	return &Canxium{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
	}
}

// NewFakeFailer creates a ethash consensus engine with a fake PoW scheme that
// accepts all blocks as valid apart from the single one specified, though they
// still have to conform to the Ethereum consensus rules.
func NewFakeFailer(fail uint64) *Canxium {
	return &Canxium{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
		fakeFail: fail,
	}
}

// NewFakeDelayer creates a ethash consensus engine with a fake PoW scheme that
// accepts all blocks as valid, but delays verifications by some time, though
// they still have to conform to the Ethereum consensus rules.
func NewFakeDelayer(delay time.Duration) *Canxium {
	return &Canxium{
		config: Config{
			PowMode: ModeFake,
			Log:     log.Root(),
		},
		fakeDelay: delay,
	}
}

// NewFullFaker creates an ethash consensus engine with a full fake scheme that
// accepts all blocks as valid, without checking any consensus rules whatsoever.
func NewFullFaker() *Canxium {
	return &Canxium{
		config: Config{
			PowMode: ModeFullFake,
			Log:     log.Root(),
		},
	}
}

// NewShared creates a full sized ethash PoW shared between all requesters running
// in the same process.
func NewShared() *Canxium {
	return &Canxium{shared: sharedEthash}
}

// Close closes the exit channel to notify all backend threads exiting.
func (canxium *Canxium) Close() error {
	return canxium.StopRemoteSealer()
}

// StopRemoteSealer stops the remote sealer
func (canxium *Canxium) StopRemoteSealer() error {
	canxium.closeOnce.Do(func() {
		// Short circuit if the exit channel is not allocated.
		if canxium.remote == nil {
			return
		}
		close(canxium.remote.requestExit)
		<-canxium.remote.exitCh
	})
	return nil
}

// Threads returns the number of mining threads currently enabled. This doesn't
// necessarily mean that mining is running!
func (canxium *Canxium) Threads() int {
	canxium.lock.Lock()
	defer canxium.lock.Unlock()

	return canxium.threads
}

// SetThreads updates the number of mining threads currently enabled. Calling
// this method does not start mining, only sets the thread count. If zero is
// specified, the miner will use all cores of the machine. Setting a thread
// count below zero is allowed and will cause the miner to idle, without any
// work being done.
func (canxium *Canxium) SetThreads(threads int) {
	canxium.lock.Lock()
	defer canxium.lock.Unlock()

	// If we're running a shared PoW, set the thread count on that instead
	if canxium.shared != nil {
		canxium.shared.SetThreads(threads)
		return
	}
	// Update the threads and ping any running seal to pull in any changes
	canxium.threads = threads
	select {
	case canxium.update <- struct{}{}:
	default:
	}
}

// Hashrate implements PoW, returning the measured rate of the search invocations
// per second over the last minute.
// Note the returned hashrate includes local hashrate, but also includes the total
// hashrate of all remote miner.
func (canxium *Canxium) Hashrate() float64 {
	// Short circuit if we are run the ethash in normal/test mode.
	if canxium.config.PowMode != ModeNormal && canxium.config.PowMode != ModeTest {
		return canxium.hashrate.Rate1()
	}
	var res = make(chan uint64, 1)

	select {
	case canxium.remote.fetchRateCh <- res:
	case <-canxium.remote.exitCh:
		// Return local hashrate only if ethash is stopped.
		return canxium.hashrate.Rate1()
	}

	// Gather total submitted hash rate of remote sealers.
	return canxium.hashrate.Rate1() + float64(<-res)
}

// APIs implements consensus.Engine, returning the user facing RPC APIs.
func (canxium *Canxium) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	// In order to ensure backward compatibility, we exposes ethash RPC APIs
	// to both eth and ethash namespaces.
	return []rpc.API{
		{
			Namespace: "eth",
			Service:   &EthAPI{canxium}, // for ethash algorithm compatibility
		},
		{
			Namespace: "ethash",
			Service:   &EthAPI{canxium}, // for ethash algorithm compatibility
		},
		{
			Namespace: "canxium", // for ethash and other algorithms
			Service:   &EthAPI{canxium},
		},
	}
}

// SeedHash is the seed to use for generating a verification cache and the mining
// dataset.
func SeedHash(block uint64) []byte {
	// return seedHash(block)
	return make([]byte, 0)
}
