// Copyright 2015 The go-ethereum Authors
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

package miner

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/cpow"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	// resultQueueSize is the size of channel listening to sealing result.
	resultQueueSize = 10

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	// resubmitAdjustChanSize is the size of resubmitting interval adjustment channel.
	resubmitAdjustChanSize = 10

	// sealingLogAtDepth is the number of confirmations before logging successful sealing.
	sealingLogAtDepth = 7

	// minRecommitInterval is the minimal time interval to recreate the sealing block with
	// any newly arrived transactions.
	minRecommitInterval = 1 * time.Second

	// maxRecommitInterval is the maximum time interval to recreate the sealing block with
	// any newly arrived transactions.
	maxRecommitInterval = 15 * time.Second

	// intervalAdjustRatio is the impact a single interval adjustment has on sealing work
	// resubmitting interval.
	intervalAdjustRatio = 0.1

	// intervalAdjustBias is applied during the new resubmit interval calculation in favor of
	// increasing upper limit or decreasing lower limit so that the limit can be reachable.
	intervalAdjustBias = 200 * 1000.0 * 1000.0

	// staleThreshold is the maximum depth of the acceptable stale block.
	staleThreshold = 7
)

var (
	errBlockInterruptedByNewHead  = errors.New("new head arrived while building block")
	errBlockInterruptedByRecommit = errors.New("recommit interrupt while building block")
	errBlockInterruptedByTimeout  = errors.New("timeout while building block")
)

// environment is the worker's current environment and holds all
// information of the sealing block generation.
type environment struct {
	signer types.Signer

	state         *state.StateDB
	tcount        int           // tx count in cycle
	miningTxcount int64         // number of mining txs in cycle
	gasPool       *core.GasPool // available gas used to pack transactions
	coinbase      common.Address

	header         *types.Header
	parentSealHash common.Hash
	txs            []*types.Transaction
	receipts       []*types.Receipt
	proposal       *types.FutureProposal
}

// copy creates a deep copy of environment.
func (env *environment) copy() *environment {
	cpy := &environment{
		signer:         env.signer,
		state:          env.state.Copy(),
		tcount:         env.tcount,
		miningTxcount:  env.miningTxcount,
		coinbase:       env.coinbase,
		header:         types.CopyHeader(env.header),
		parentSealHash: env.parentSealHash,
		receipts:       copyReceipts(env.receipts),
	}
	if env.gasPool != nil {
		gasPool := *env.gasPool
		cpy.gasPool = &gasPool
	}
	// The content of txs is immutable, unnecessary to do the expensive deep copy.
	cpy.txs = make([]*types.Transaction, len(env.txs))
	copy(cpy.txs, env.txs)
	return cpy
}

// discard terminates the background prefetcher go-routine. It should
// always be called for all created environment instances otherwise
// the go-routine leak can happen.
func (env *environment) discard() {
	if env.state == nil {
		return
	}
	env.state.StopPrefetcher()
}

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts     []*types.Receipt
	state        *state.StateDB
	block        *types.Block
	proposal     *types.FutureProposal
	proposalHash *common.Hash
	createdAt    time.Time

	// PoW 2.0 proposal coordination.
	// proposalStop is closed by resultLoop when the sealed block arrives,
	// signalling buildProposalForCurrentEnv to stop collecting and finalise.
	// proposalReady is closed by buildProposalForCurrentEnv once task.proposal
	// has been written, signalling resultLoop that it may proceed.
	// The Once guards ensure each channel is closed exactly once even when
	// commit() is called multiple times for the same seal hash (resubmission).
	proposalStop      chan struct{}
	proposalStopOnce  sync.Once
	proposalReady     chan struct{}
	proposalReadyOnce sync.Once
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
	commitInterruptTimeout
)

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interrupt *atomic.Int32
	timestamp int64
}

// newPayloadResult represents a result struct corresponds to payload generation.
type newPayloadResult struct {
	err   error
	block *types.Block
	fees  *big.Int
}

// getWorkReq represents a request for getting a new sealing work with provided parameters.
type getWorkReq struct {
	params *generateParams
	result chan *newPayloadResult // non-blocking channel
}

// intervalAdjust represents a resubmitting interval adjustment.
type intervalAdjust struct {
	ratio float64
	inc   bool
}

// worker is the main object which takes care of submitting new work to consensus engine
// and gathering the sealing result.
type worker struct {
	config      *Config
	chainConfig *params.ChainConfig
	engine      consensus.Engine
	eth         Backend
	chain       *core.BlockChain

	// Feeds
	pendingLogsFeed event.Feed

	// Subscriptions
	mux          *event.TypeMux
	txsCh        chan core.NewTxsEvent
	txsSub       event.Subscription
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	// Channels
	newWorkCh          chan *newWorkReq
	getWorkCh          chan *getWorkReq
	taskCh             chan *task
	resultCh           chan *types.Block
	startCh            chan struct{}
	exitCh             chan struct{}
	resubmitIntervalCh chan time.Duration
	resubmitAdjustCh   chan *intervalAdjust

	wg sync.WaitGroup

	current     *environment       // An environment for current running cycle.
	next        *environment       // An environment for next cycle which is being built in background.
	unconfirmed *unconfirmedBlocks // A set of locally mined blocks pending canonicalness confirmations.

	mu       sync.RWMutex // The lock used to protect the coinbase and extra fields
	coinbase common.Address
	extra    []byte

	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	snapshotMu       sync.RWMutex // The lock used to protect the snapshots below
	snapshotBlock    *types.Block
	snapshotReceipts types.Receipts
	snapshotState    *state.StateDB

	// atomic status counters
	running atomic.Bool  // The indicator whether the consensus engine is running or not.
	newTxs  atomic.Int32 // New arrival transaction count since last sealing work submitting.

	// newpayloadTimeout is the maximum timeout allowance for creating payload.
	// The default value is 2 seconds but node operator can set it to arbitrary
	// large value. A large timeout allowance may cause Geth to fail creating
	// a non-empty payload within the specified time and eventually miss the slot
	// in case there are some computation expensive transactions in txpool.
	newpayloadTimeout time.Duration

	// recommit is the time interval to re-create sealing work or to re-build
	// payload in proof-of-stake stage.
	recommit time.Duration

	// External functions
	isLocalBlock func(header *types.Header) bool // Function used to determine whether the specified block is mined by local miner.

	// proposalKey is the ECDSA key used to sign the N+2 FutureProposal Root.
	// Parsed once from Config.Private at startup; nil means proposals are unsigned.
	proposalKey *ecdsa.PrivateKey

	// Test hooks
	newTaskHook  func(*task)                        // Method to call upon receiving a new sealing task.
	skipSealHook func(*task) bool                   // Method to decide whether skipping the sealing.
	fullTaskHook func()                             // Method to call before pushing the full sealing task.
	resubmitHook func(time.Duration, time.Duration) // Method to call upon updating resubmitting interval.
}

func newWorker(config *Config, chainConfig *params.ChainConfig, engine consensus.Engine, eth Backend, mux *event.TypeMux, isLocalBlock func(header *types.Header) bool, init bool) *worker {
	worker := &worker{
		config:             config,
		chainConfig:        chainConfig,
		engine:             engine,
		eth:                eth,
		chain:              eth.BlockChain(),
		mux:                mux,
		isLocalBlock:       isLocalBlock,
		unconfirmed:        newUnconfirmedBlocks(eth.BlockChain(), sealingLogAtDepth),
		coinbase:           config.Etherbase,
		extra:              config.ExtraData,
		pendingTasks:       make(map[common.Hash]*task),
		txsCh:              make(chan core.NewTxsEvent, txChanSize),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		newWorkCh:          make(chan *newWorkReq),
		getWorkCh:          make(chan *getWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		startCh:            make(chan struct{}, 1),
		exitCh:             make(chan struct{}),
		resubmitIntervalCh: make(chan time.Duration),
		resubmitAdjustCh:   make(chan *intervalAdjust, resubmitAdjustChanSize),
	}

	// Parse the proposal signing key if provided.
	if len(config.Private) <= 0 && config.Etherbase != (common.Address{}) {
		log.Error("No miner private key provided for the etherbase, worker won't be able to sign future proposals", "etherbase", config.Etherbase.Hex())
		os.Exit(1)
	} else if len(config.Private) > 0 && config.Etherbase != (common.Address{}) {
		key, err := crypto.ToECDSA(config.Private)
		if err != nil {
			log.Error("Invalid miner private key for proposal signing, proposals will be unsigned", "err", err)
			os.Exit(1)
		}

		worker.proposalKey = key
		log.Info("Proposal signing key loaded", "address", crypto.PubkeyToAddress(key.PublicKey))
	}

	// Subscribe NewTxsEvent for tx pool
	worker.txsSub = eth.TxPool().SubscribeNewTxsEvent(worker.txsCh)
	// Subscribe events for blockchain
	worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)

	// Sanitize recommit interval if the user-specified one is too short.
	recommit := worker.config.Recommit
	if recommit < minRecommitInterval {
		log.Warn("Sanitizing miner recommit interval", "provided", recommit, "updated", minRecommitInterval)
		recommit = minRecommitInterval
	}
	worker.recommit = recommit

	// Sanitize the timeout config for creating payload.
	newpayloadTimeout := worker.config.NewPayloadTimeout
	if newpayloadTimeout == 0 {
		log.Warn("Sanitizing new payload timeout to default", "provided", newpayloadTimeout, "updated", DefaultConfig.NewPayloadTimeout)
		newpayloadTimeout = DefaultConfig.NewPayloadTimeout
	}
	if newpayloadTimeout < time.Millisecond*100 {
		log.Warn("Low payload timeout may cause high amount of non-full blocks", "provided", newpayloadTimeout, "default", DefaultConfig.NewPayloadTimeout)
	}
	worker.newpayloadTimeout = newpayloadTimeout

	worker.wg.Add(4)
	go worker.mainLoop()
	go worker.newWorkLoop(recommit)
	go worker.resultLoop()
	go worker.taskLoop()

	// Submit first work to initialize pending state.
	if init {
		worker.startCh <- struct{}{}
	}
	return worker
}

// setEtherbase sets the etherbase used to initialize the block coinbase field.
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

// etherbase retrieves the configured etherbase address.
func (w *worker) etherbase() common.Address {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.coinbase
}

func (w *worker) setGasCeil(ceil uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.GasCeil = ceil
}

// setExtra sets the content used to initialize the block extra field.
func (w *worker) setExtra(extra []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extra = extra
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *worker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

// pending returns the pending state and corresponding block.
func (w *worker) pending() (*types.Block, *state.StateDB) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendingBlock returns pending block.
func (w *worker) pendingBlock() *types.Block {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// pendingBlockAndReceipts returns pending block and corresponding receipts.
func (w *worker) pendingBlockAndReceipts() (*types.Block, types.Receipts) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock, w.snapshotReceipts
}

// start sets the running status as 1 and triggers new work submitting.
func (w *worker) start() {
	w.running.Store(true)
	w.startCh <- struct{}{}
}

// stop sets the running status as 0.
func (w *worker) stop() {
	w.running.Store(false)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return w.running.Load()
}

// close terminates all background threads maintained by the worker.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	w.running.Store(false)
	close(w.exitCh)
	w.wg.Wait()
}

// recalcRecommit recalculates the resubmitting interval upon feedback.
func recalcRecommit(minRecommit, prev time.Duration, target float64, inc bool) time.Duration {
	var (
		prevF = float64(prev.Nanoseconds())
		next  float64
	)
	if inc {
		next = prevF*(1-intervalAdjustRatio) + intervalAdjustRatio*(target+intervalAdjustBias)
		max := float64(maxRecommitInterval.Nanoseconds())
		if next > max {
			next = max
		}
	} else {
		next = prevF*(1-intervalAdjustRatio) + intervalAdjustRatio*(target-intervalAdjustBias)
		min := float64(minRecommit.Nanoseconds())
		if next < min {
			next = min
		}
	}
	return time.Duration(int64(next))
}

// newWorkLoop is a standalone goroutine to submit new sealing work upon received events.
func (w *worker) newWorkLoop(recommit time.Duration) {
	defer w.wg.Done()
	var (
		interrupt   *atomic.Int32
		minRecommit = recommit // minimal resubmit interval specified by user.
		timestamp   int64      // timestamp for each round of sealing.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(s int32) {
		if interrupt != nil {
			interrupt.Store(s)
		}
		interrupt = new(atomic.Int32)
		select {
		case w.newWorkCh <- &newWorkReq{interrupt: interrupt, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
		w.newTxs.Store(0)
	}
	// clearPending cleans the stale pending tasks.
	clearPending := func(number uint64) {
		w.pendingMu.Lock()
		for h, t := range w.pendingTasks {
			if t.block.NumberU64()+staleThreshold <= number {
				delete(w.pendingTasks, h)
			}
		}
		w.pendingMu.Unlock()
	}

	for {
		select {
		case <-w.startCh:
			clearPending(w.chain.CurrentBlock().Number.Uint64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case head := <-w.chainHeadCh:
			clearPending(head.Block.NumberU64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() && (w.chainConfig.Clique == nil || w.chainConfig.Clique.Period > 0) {
				// Short circuit if no new transaction arrives.
				if w.newTxs.Load() == 0 {
					timer.Reset(recommit)
					continue
				}
				commit(commitInterruptResubmit)
			}

		case interval := <-w.resubmitIntervalCh:
			// Adjust resubmit interval explicitly by user.
			if interval < minRecommitInterval {
				log.Warn("Sanitizing miner recommit interval", "provided", interval, "updated", minRecommitInterval)
				interval = minRecommitInterval
			}
			log.Info("Miner recommit interval update", "from", minRecommit, "to", interval)
			minRecommit, recommit = interval, interval

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case adjust := <-w.resubmitAdjustCh:
			// Adjust resubmit interval by feedback.
			if adjust.inc {
				before := recommit
				target := float64(recommit.Nanoseconds()) / adjust.ratio
				recommit = recalcRecommit(minRecommit, recommit, target, true)
				log.Trace("Increase miner recommit interval", "from", before, "to", recommit)
			} else {
				before := recommit
				recommit = recalcRecommit(minRecommit, recommit, float64(minRecommit.Nanoseconds()), false)
				log.Trace("Decrease miner recommit interval", "from", before, "to", recommit)
			}

			if w.resubmitHook != nil {
				w.resubmitHook(minRecommit, recommit)
			}

		case <-w.exitCh:
			return
		}
	}
}

// mainLoop is responsible for generating and submitting sealing work based on
// the received event. It can support two modes: automatically generate task and
// submit it or return task according to given parameters for various proposes.
func (w *worker) mainLoop() {
	defer w.wg.Done()
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer func() {
		w.mu.Lock()
		if w.current != nil {
			w.current.discard()
			w.current = nil
		}
		if w.next != nil {
			w.next.discard()
			w.next = nil
		}
		w.mu.Unlock()
	}()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interrupt, req.timestamp)

		// Drain incoming tx-notification events. In the N+2 pipeline the tx
		// list for the current block is fixed (from the N-2 proposal), so we
		// don't need to act on these events. We MUST drain the channel though:
		// event.Feed.Send blocks until every subscriber has received, so an
		// unread txsCh eventually fills up (cap 4096) and permanently stalls
		// the txpool's runReorg goroutine, which in turn prevents demoting
		// already-included transactions from pending, causing pool.mu lock
		// starvation that blocks eth_sendRawTransaction for 30+ seconds.
		case <-w.txsCh:
			// intentionally empty — just keep the channel drained.

		// System stopped
		case <-w.exitCh:
			return
		case <-w.txsSub.Err():
			return
		case <-w.chainHeadSub.Err():
			return
		}
	}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *worker) taskLoop() {
	defer w.wg.Done()
	var (
		stopCh   chan struct{}
		stopOnce *sync.Once
		prev     common.Hash
	)

	// interrupt aborts the in-flight sealing task. The Once is shared with the
	// prepareNextWork goroutine so either side can close stopCh idempotently.
	interrupt := func() {
		if stopOnce != nil {
			stopOnce.Do(func() { close(stopCh) })
		}
	}
	for {
		select {
		case task := <-w.taskCh:
			if w.newTaskHook != nil {
				w.newTaskHook(task)
			}
			// Reject duplicate sealing work due to resubmitting.
			sealHash := w.engine.SealHash(task.block.Header())
			if sealHash == prev {
				continue
			}
			// Interrupt previous sealing operation
			interrupt()
			stopCh, stopOnce, prev = make(chan struct{}), &sync.Once{}, sealHash

			if w.skipSealHook != nil && w.skipSealHook(task) {
				continue
			}
			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = task
			w.pendingMu.Unlock()

			// Kick off background pre-execution for the next block only after the
			// task has been accepted by taskLoop, so pendingTasks[sealHash] is
			// guaranteed to exist when buildProposalForCurrentEnv looks it up.
			// If pre-execution fails (reorg, missing grandparent, etc.) we MUST
			// abort the in-flight seal: without a valid proposal, resultLoop would
			// commit a malformed block. The next commitWork event will rebuild and
			// retry from a fresh head. Capture stopCh/stopOnce by value because the
			// outer locals get reassigned on the next task.
			go func(abortCh chan struct{}, abortOnce *sync.Once) {
				if err := w.prepareNextWork(sealHash); err != nil {
					log.Warn("Failed to prepare next work, aborting in-flight seal", "err", err)
					abortOnce.Do(func() { close(abortCh) })
				}
			}(stopCh, stopOnce)

			// CIP-0003: push miner nonce range to ethash before sealing
			blockNum := task.block.NumberU64()
			miner := w.eth.BlockChain().WdcCache.GetMiner(w.coinbase, blockNum)
			if miner == nil {
				log.Warn("No miner found for sealing", "number", blockNum, "coinbase", w.coinbase.Hex())
				return
			}

			if err := w.engine.Seal(w.chain, miner.Start, miner.End, task.state, task.block, w.resultCh, stopCh); err != nil {
				log.Warn("Block sealing failed", "err", err)
				w.pendingMu.Lock()
				delete(w.pendingTasks, sealHash)
				w.pendingMu.Unlock()
			}
		case <-w.exitCh:
			interrupt()
			return
		}
	}
}

// resultLoop is a standalone goroutine to handle sealing result submitting
// and flush relative data to the database.
func (w *worker) resultLoop() {
	defer w.wg.Done()
	for {
		select {
		case block := <-w.resultCh:
			// Short circuit when receiving empty result.
			if block == nil {
				continue
			}
			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				continue
			}
			var (
				header   = block.Header()
				sealhash = w.engine.SealHash(header)
			)
			w.pendingMu.RLock()
			task, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash)
				continue
			}

			// Tell buildProposalForCurrentEnv to stop collecting and finalise now.
			task.proposalStopOnce.Do(func() { close(task.proposalStop) })

			log.Info("Block sealed, waiting for finalising proposal", "number", block.Number(), "sealhash", sealhash, "elapsed", common.PrettyDuration(time.Since(task.createdAt)))
			// Wait for buildProposalForCurrentEnv to write task.proposal.
			select {
			case <-task.proposalReady:
			case <-w.exitCh:
				return
			}

			// If prepareNextWork failed, proposalReady is closed but task.proposal
			// is nil. Committing a block without a proposal produces malformed RLP
			// on import. Discard and let commitWork rebuild from a fresh head.
			if task.proposal == nil || task.proposalHash == nil {
				log.Warn("Sealed block has no proposal, discarding", "number", block.Number(), "sealhash", sealhash)
				continue
			}

			header.ProposalHash = task.proposalHash
			// Update the block with proposal since the block generated by consensus engine may not contain the proposal.
			block = block.WithProposal(task.proposal).WithSeal(header)

			var hash = block.Hash()

			log.Info("Block sealed, proposal finalised", "number", block.Number(), "sealhash", sealhash, "hash", hash, "elapsed", common.PrettyDuration(time.Since(task.createdAt)))

			w.mu.Lock()
			if w.next != nil && w.next.parentSealHash == sealhash {
				w.next.header.ParentHash = hash
			}
			w.mu.Unlock()

			// Different block could share same sealhash, deep copy here to prevent write-write conflict.
			var (
				receipts = make([]*types.Receipt, len(task.receipts))
				logs     []*types.Log
			)
			for i, taskReceipt := range task.receipts {
				receipt := new(types.Receipt)
				receipts[i] = receipt
				*receipt = *taskReceipt

				// add block location fields
				receipt.BlockHash = hash
				receipt.BlockNumber = block.Number()
				receipt.TransactionIndex = uint(i)

				// Update the block hash in all logs since it is now available and not when the
				// receipt/log of individual transactions were created.
				receipt.Logs = make([]*types.Log, len(taskReceipt.Logs))
				for i, taskLog := range taskReceipt.Logs {
					log := new(types.Log)
					receipt.Logs[i] = log
					*log = *taskLog
					log.BlockHash = hash
				}
				logs = append(logs, receipt.Logs...)
			}

			// Commit block and state to database.
			_, err := w.chain.WriteBlockAndSetHead(block, receipts, logs, task.state, true)
			if err != nil {
				log.Error("Failed writing block to chain", "err", err)
				continue
			}
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", hash, "root", block.Root(),
				"elapsed", common.PrettyDuration(time.Since(task.createdAt)), "nonce", block.Header().Nonce.Uint64())

			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

			// Insert the block into the set of pending ones to resultLoop for confirmations
			w.unconfirmed.Insert(block.NumberU64(), block.Hash())

		case <-w.exitCh:
			return
		}
	}
}

// makeEnv creates a new environment for the sealing block.
func (w *worker) makeEnv(parentState *state.StateDB, header *types.Header, coinbase common.Address) (*environment, error) {
	parentState.StartPrefetcher("miner")

	// Note the passed coinbase may be different with header.Coinbase.
	return &environment{
		signer:   types.MakeSigner(w.chainConfig, header.Number),
		state:    parentState,
		coinbase: coinbase,
		header:   header,
	}, nil
}

// updateSnapshot updates pending snapshot block, receipts and state.
func (w *worker) updateSnapshot(env *environment) {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	w.snapshotBlock = types.NewBlock(
		env.header,
		env.txs,
		nil,
		env.receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = copyReceipts(env.receipts)
	w.snapshotState = env.state.Copy()
}

func (w *worker) commitTransaction(env *environment, tx *types.Transaction) ([]*types.Log, error) {
	var (
		snap = env.state.Snapshot()
		gp   = env.gasPool.Gas()
	)
	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &env.coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *w.chain.GetVMConfig())
	if err != nil {
		env.state.RevertToSnapshot(snap)
		env.gasPool.SetGas(gp)
		return nil, err
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransactions(env *environment, txs []common.Hash, interrupt *atomic.Int32) error {
	gasLimit := env.header.GasLimit
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(gasLimit)
	}
	var coalescedLogs []*types.Log

	for _, hash := range txs {
		// Check interruption signal and abort building if it's fired.
		if interrupt != nil {
			if signal := interrupt.Load(); signal != commitInterruptNone {
				return signalToErr(signal)
			}
		}
		// If we don't have enough gas for any further transactions then we're done.
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}
		tx := w.eth.TxPool().Get(hash)
		if tx == nil {
			// TODO: Try to get the transaction from the network before giving up!
			return fmt.Errorf("transaction disappeared from pool: %s", hash)
		}

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		from, _ := types.Sender(env.signer, tx)

		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)
			continue
		}
		if tx.IsMiningTx() {
			if env.miningTxcount >= cpow.MaxMiningTransactionPerBlock {
				log.Trace("Ignoring mining transaction, out of slot", "hash", tx.Hash(), "current", env.miningTxcount, "max", cpow.MaxMiningTransactionPerBlock)
				continue
			}

			reward := big.NewInt(0)
			if tx.Type() == types.MiningTxType {
				// skip old mining transaction have different mining reward, not match this period
				subsidy := cpow.TransactionMiningSubsidy(w.chainConfig, env.header.Number)
				reward = new(big.Int).Mul(subsidy, tx.Difficulty())
			} else if tx.Type() == types.CrossMiningTxType {
				forkTime := cpow.CrossMiningForkTime(w.chainConfig, tx.AuxPoW().Chain())
				isLithiumFork := w.chainConfig.IsLithium(env.header.Time)
				reward = cpow.CrossMiningReward(isLithiumFork, tx.AuxPoW(), forkTime, env.header.Time)
			}

			if tx.Value().Cmp(reward) != 0 {
				log.Trace("Ignoring mining transaction, not match subsidy period", "hash", tx.Hash(), "tx value", tx.Value(), "subsidy", reward)
				continue
			}
		}
		// Start executing the transaction
		env.state.SetTxContext(tx.Hash(), env.tcount)

		logs, err := w.commitTransaction(env, tx)
		switch {
		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			if tx.IsMiningTx() {
				env.miningTxcount++
			}

		default:
			// Transaction is regarded as invalid, drop all consecutive transactions from
			// the same sender because of `nonce-too-high` clause.
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
		}
	}

	if !w.isRunning() && len(coalescedLogs) > 0 {
		// We don't push the pendingLogsEvent while we are sealing. The reason is that
		// when we are sealing, the worker will regenerate a sealing block every 3 seconds.
		// In order to avoid pushing the repeated pendingLog, we disable the pending log pushing.

		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		w.pendingLogsFeed.Send(cpy)
	}
	return nil
}

// generateParams wraps various of settings for generating sealing task.
type generateParams struct {
	timestamp   uint64            // The timstamp for sealing task
	forceTime   bool              // Flag whether the given timestamp is immutable or not
	parentHash  common.Hash       // Parent block hash, empty means the latest chain head
	coinbase    common.Address    // The fee recipient address for including transaction
	random      common.Hash       // The randomness generated by beacon chain, empty before the merge
	withdrawals types.Withdrawals // List of withdrawals to include in block.
	noUncle     bool              // Flag whether the uncle block inclusion is allowed
	noTxs       bool              // Flag whether an empty block without any transaction is expected
}

// prepareWork constructs the sealing task according to the given parameters,
// either based on the last chain head or specified parent. In this function
// the pending transactions are not filled yet, only the empty task returned.
func (w *worker) prepareWork(genParams *generateParams) (*environment, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Find the parent block for sealing task
	parent := w.chain.CurrentBlock()
	if parent == nil {
		return nil, fmt.Errorf("missing parent")
	}
	// Time must be strictly greater than parent and identical across all nodes
	// so that every node produces the same SealHash for the same block.
	if genParams.forceTime && genParams.timestamp != parent.Time+1 {
		return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time, genParams.timestamp)
	}
	// Construct the sealing block header.
	// Extra is intentionally left empty: node-specific extra data would make
	// SealHash differ across nodes, breaking distributed PoW 2.0 verification.
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit, w.config.GasCeil),
		Time:       parent.Time + 2,
		Coinbase:   genParams.coinbase,
	}
	// Set the randomness field from the beacon chain if it's available.
	if genParams.random != (common.Hash{}) {
		header.MixDigest = genParams.random
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = misc.CalcBaseFee(w.chainConfig, parent)
		if !w.chainConfig.IsLondon(parent.Number) {
			parentGasLimit := parent.GasLimit * w.chainConfig.ElasticityMultiplier()
			header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		}
	}
	// Run the consensus preparation with the default or customized consensus engine.
	if err := w.engine.Prepare(w.chain, header); err != nil {
		log.Error("Failed to prepare header for sealing", "err", err)
		return nil, err
	}
	// Could potentially happen if starting to mine in an odd state.
	// Note genParams.coinbase can be different with header.Coinbase
	// since clique algorithm can modify the coinbase field in header.
	state, err := w.chain.StateAt(parent.Root)
	if err != nil {
		return nil, err
	}

	env, err := w.makeEnv(state, header, genParams.coinbase)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}

	return env, nil
}

// prepareNextWork constructs the next sealing task (Block N+1) based on w.current (Block N).
// It pre-executes the transactions proposed in the grandparent block's proposal (N-1 proposal
// which defines N+1's tx list), and builds the N+2 proposal for w.current.
// Caller must NOT hold w.mu.
func (w *worker) prepareNextWork(previousSealHash common.Hash) (retErr error) {
	// On any error return, unblock resultLoop which may already be waiting on
	// task.proposalReady after receiving the sealed block.
	defer func() {
		if retErr != nil {
			w.pendingMu.RLock()
			task, exists := w.pendingTasks[previousSealHash]
			w.pendingMu.RUnlock()
			if exists {
				task.proposalReadyOnce.Do(func() { close(task.proposalReady) })
			}
		}
	}()

	w.mu.RLock()
	previous := w.current
	w.mu.RUnlock()

	if previous == nil {
		return fmt.Errorf("missing previous environment")
	}

	// Find the parent block for sealing task (grandparent of the next block).
	grandParent := w.chain.GetBlockByHash(previous.header.ParentHash)
	if grandParent == nil {
		return fmt.Errorf("missing grand parent block %s", previous.header.ParentHash)
	}

	// Construct the sealing block header for Block N+1.
	// Block N+1's timestamp must be strictly greater than Block N's timestamp.
	// prepareNextWork is called right after commit(), so time.Now() may still
	// be the same second as previous.header.Time — apply the same lower-bound
	// guard that prepareWork uses.
	header := &types.Header{
		ParentHash: common.Hash{}, // parent hash is not known until the block is sealed
		Number:     new(big.Int).Add(previous.header.Number, common.Big1),
		GasLimit:   core.CalcGasLimit(previous.header.GasLimit, w.config.GasCeil),
		Time:       previous.header.Time + 2,
		Coinbase:   cpow.WdcAddress,
		// Extra intentionally empty for SealHash consistency across nodes.
	}
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = misc.CalcBaseFee(w.chainConfig, previous.header)
		if !w.chainConfig.IsLondon(previous.header.Number) {
			parentGasLimit := previous.header.GasLimit * w.chainConfig.ElasticityMultiplier()
			header.GasLimit = core.CalcGasLimit(parentGasLimit, w.config.GasCeil)
		}
	}
	// previous.header.UncleHash is the zero value until FinalizeAndAssemble sets it
	// via types.NewBlock. CalcDifficulty branches on EmptyUncleHash vs anything
	// else, so we must populate it explicitly. PoW 2.0 has no uncles, so the hash
	// is always EmptyUncleHash.
	parentForDiff := *previous.header
	parentForDiff.UncleHash = types.EmptyUncleHash
	header.Difficulty = ethash.CalcDifficulty(w.chainConfig, header.Time, &parentForDiff)

	w.pendingMu.RLock()
	task, exists := w.pendingTasks[previousSealHash]
	w.pendingMu.RUnlock()

	if !exists {
		return fmt.Errorf("missing pending task for previous seal hash %s", previousSealHash)
	}

	// Fetch the state after Block N (w.current's state already has N applied).
	// Copy task.state: resultLoop will pass task.state to WriteBlockAndSetHead
	// for block N, so any pre-execution mutations on the shared StateDB would
	// pollute the committed state and break the snapshot tree (the snap layer
	// would be registered for a root that doesn't match block N's header root,
	// leaving the canonical root absent from the snapshot).
	env, err := w.makeEnv(task.state.Copy(), header, cpow.WdcAddress)
	if err != nil {
		log.Error("Failed to create next sealing context", "err", err)
		return err
	}

	// The tx list for Block N+1 was fixed by the producer of Block N-1 (grandParent).
	if grandParent.Body().Proposal == nil {
		// No proposal from grandparent — this can happen at chain genesis or during
		// protocol bootstrap. Proceed with an empty block for N+1.
		log.Error("Grandparent has no N+2 proposal, this should only happen at genesis or during protocol bootstrap, proceeding with empty block",
			"grandparent", grandParent.NumberU64())
	} else {
		// Pre-execute N+1 transactions.
		err = w.commitTransactions(env, grandParent.Body().Proposal.TxHashes, nil)
		switch {
		case err == nil:
			select {
			case w.resubmitAdjustCh <- &intervalAdjust{inc: false}:
			default:
			}
		case errors.Is(err, errBlockInterruptedByRecommit):
			gaslimit := env.header.GasLimit
			ratio := float64(gaslimit-env.gasPool.Gas()) / float64(gaslimit)
			if ratio < 0.1 {
				ratio = 0.1
			}
			select {
			case w.resubmitAdjustCh <- &intervalAdjust{ratio: ratio, inc: true}:
			default:
			}
		case errors.Is(err, errBlockInterruptedByNewHead):
			// A new head arrived while we were building; discard and let
			// commitWork rebuild everything from scratch.
			env.discard()
			return err
		default:
			// Any other error (e.g. tx disappeared from pool): log and continue
			// with whatever transactions were committed successfully so far.
			log.Warn("Error during next-work pre-execution, partial block", "err", err)
		}
	}

	w.mu.Lock()
	// Make sure w.current hasn't been replaced by a reorg while we were executing.
	if w.current != previous {
		w.mu.Unlock()
		env.discard()
		return fmt.Errorf("current environment changed during next-work preparation, discarding")
	}
	w.next = env
	w.next.parentSealHash = previousSealHash
	w.mu.Unlock()

	log.Info("Prepared next work for Block N+1",
		"current block", previous.header.Number,
		"next block", env.header.Number,
		"tx count", len(env.txs),
		"previous seal hash", previousSealHash,
	)

	w.buildProposalForCurrentEnv(previousSealHash)
	return nil
}

// buildProposalForCurrentEnv selects pending transactions for Block N+2 and attaches
// the resulting FutureProposal to w.current (Block N). This is called after
// prepareNextWork has populated w.next (Block N+1 pre-execution).
// Caller must NOT hold w.mu.
func (w *worker) buildProposalForCurrentEnv(previousSealHash common.Hash) {
	w.mu.RLock()
	current := w.current
	next := w.next
	w.mu.RUnlock()

	if current == nil || next == nil {
		return
	}

	// Retrieve the task so we can observe proposalStop and signal proposalReady.
	w.pendingMu.RLock()
	task, taskExists := w.pendingTasks[previousSealHash]
	w.pendingMu.RUnlock()

	if !taskExists {
		log.Warn("No pending task for current block, skipping proposal building", "currentBlock", current.header.Number, "sealHash", previousSealHash)
		return
	}

	// Use a snapshot of next's gas pool so we don't mutate the pre-executed state.
	var availableGas uint64
	if next.gasPool != nil {
		availableGas = next.gasPool.Gas()
	} else {
		availableGas = next.header.GasLimit
	}
	remainingGas := availableGas

	// Get pending transactions from the pool
	pending := w.eth.TxPool().Pending(true)
	var selectedTxHashes []common.Hash

outer:
	for address, accountTxs := range pending {
		nextNonce := next.state.GetNonce(address)
		accumulatedCost := new(big.Int)
		for _, tx := range accountTxs {
			select {
			case <-task.proposalStop:
				break outer
			default:
			}

			// Skip already-executed transactions that the pool hasn't evicted yet.
			if tx.Nonce() < nextNonce {
				continue
			}

			// Quick validation: gas limit against remaining gas budget.
			if tx.Gas() > remainingGas {
				break
			}

			// Nonces must be strictly sequential.
			if tx.Nonce() != nextNonce {
				log.Warn("Skipping transaction for N+2 proposal due to nonce gap", "address", address, "expectedNonce", nextNonce, "txNonce", tx.Nonce())
				break
			}

			// Check balance for accumulated gas cost + value.
			balance := next.state.GetBalance(address)
			gasCost := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasPrice())
			cost := new(big.Int).Add(accumulatedCost, new(big.Int).Add(gasCost, tx.Value()))
			if balance.Cmp(cost) < 0 {
				log.Warn("Skipping transaction for N+2 proposal due to insufficient balance", "address", address, "balance", balance, "required", cost)
				break
			}
			accumulatedCost = cost

			// Transaction looks valid — add to proposal.
			selectedTxHashes = append(selectedTxHashes, tx.Hash())
			remainingGas -= tx.Gas()
			nextNonce++
		}
	}

	// Calculate Merkle root over the transaction hashes only (no need to fetch
	// full transaction objects from the pool).
	root := types.DeriveSha(proposalHashList(selectedTxHashes), trie.NewStackTrie(nil))

	proposal := &types.FutureProposal{
		TxHashes: selectedTxHashes,
	}

	// Sign the proposal Root with the miner's private key.
	sig, err := crypto.Sign(root[:], w.proposalKey)
	if err != nil {
		log.Warn("Failed to sign N+2 proposal", "err", err)
		return
	}

	proposal.Signature = sig

	w.mu.Lock()
	// Guard: if current was replaced by a reorg while we were selecting, discard.
	if w.current != current {
		w.mu.Unlock()
		log.Warn("Current environment changed during proposal building, discarding proposal")
		task.proposalReadyOnce.Do(func() { close(task.proposalReady) })
		return
	}
	w.current.proposal = proposal
	w.mu.Unlock()

	w.pendingMu.Lock()
	task.proposal = proposal
	task.proposalHash = &root
	w.pendingMu.Unlock()

	// Signal resultLoop that the proposal is ready.
	task.proposalReadyOnce.Do(func() { close(task.proposalReady) })

	log.Info("Generated N+2 proposal",
		"current block", current.header.Number,
		"future block", new(big.Int).Add(next.header.Number, common.Big1),
		"tx count", len(selectedTxHashes),
		"previousSealHash", previousSealHash,
	)
}

// newWork generates new sealing work (Block N) based on the latest chain head and
// submits it to the consensus engine. This is the fallback path when no valid
// pre-executed pipeline work is available in w.next.
func (w *worker) newWork(interrupt *atomic.Int32, timestamp int64) (*environment, error) {
	work, err := w.prepareWork(&generateParams{
		timestamp: uint64(timestamp),
		coinbase:  cpow.WdcAddress,
	})
	if err != nil {
		return nil, err
	}
	// Note: In PoW 2.0 the transaction list is fixed by the N-2 proposal, so
	// there is no benefit to submitting an optimistic empty block first.
	// Doing so would cause a double-commit for the same seal hash, which
	// creates a race in prepareNextWork ("current environment changed") and
	// leaves resultLoop stuck waiting for proposalReady.

	// The tx list for Block N was fixed by the producer of Block N-2.
	blockNum := work.header.Number.Uint64()
	if blockNum < 2 {
		// Genesis / very early blocks: no N-2 proposal exists yet, mine empty.
		return work, nil
	}
	proposalBlock := w.chain.GetBlockByNumber(blockNum - 2)
	if proposalBlock == nil {
		return work, nil
	}
	if proposalBlock.Body().Proposal == nil {
		return work, nil
	}

	// Fill pending transactions from the N-2 proposal.
	err = w.commitTransactions(work, proposalBlock.Body().Proposal.TxHashes, interrupt)
	switch {
	case err == nil:
		select {
		case w.resubmitAdjustCh <- &intervalAdjust{inc: false}:
		default:
		}
	case errors.Is(err, errBlockInterruptedByRecommit):
		gaslimit := work.header.GasLimit
		ratio := float64(gaslimit-work.gasPool.Gas()) / float64(gaslimit)
		if ratio < 0.1 {
			ratio = 0.1
		}
		select {
		case w.resubmitAdjustCh <- &intervalAdjust{ratio: ratio, inc: true}:
		default:
		}
	case errors.Is(err, errBlockInterruptedByNewHead):
		// New head arrived during execution; discard this work entirely.
		work.discard()
		return nil, err
	default:
		// Unexpected error (e.g. tx disappeared): log and continue with partial block.
		log.Warn("Error filling transactions from N-2 proposal, partial block", "err", err)
	}

	return work, nil
}

// commitWork generates sealing tasks for Block N and advances the pipeline.
//
// The pipeline invariant (per PoW 2.0) at steady state is:
//
//	w.current  → Block N  (being mined)
//	w.next     → Block N+1 (pre-executed, ready to seal once N is found)
//
// Fast path: w.next was built to extend the current chain head, so we promote
// it to w.current and seal it directly. Otherwise (stale pipeline or reorg)
// we discard the pre-built work and rebuild from scratch.
//
// w.commit and w.newWork run outside w.mu: both can take seconds (state copy,
// transaction execution, FinalizeAndAssemble, blocking taskCh send) and
// holding w.mu across that would stall every reader of the worker state
// (etherbase, extra, current, next).
func (w *worker) commitWork(interrupt *atomic.Int32, timestamp int64) {
	start := time.Now()

	chainHead := w.chain.CurrentBlock()
	if chainHead == nil {
		log.Error("Cannot commit work: no current chain head")
		return
	}

	chainSealHash := w.engine.SealHash(chainHead)
	log.Info("Committing new work", "head", chainHead.Number, "nonce", chainHead.Nonce.Uint64(), "head hash", chainHead.Hash(), "head seal hash", chainSealHash, "elapsed", common.PrettyDuration(time.Since(start)))

	w.mu.Lock()

	var useNext bool
	if w.next != nil {
		// w.next was pre-built to extend a specific parent block.
		// It is valid if its own parentSealHash matches the current chain head's seal hash,
		// Meaning w.next extends the current chain head and is ready to be sealed as the next block.
		if w.next.parentSealHash == chainSealHash {
			if w.next.header.ParentHash != chainHead.Hash() {
				w.next.header.ParentHash = chainHead.Hash()
			}

			useNext = true
		} else {
			// Pipeline is stale (reorg or late head). Discard and rebuild.
			log.Warn("Pre-executed next work is stale, discarding",
				"pre-build parent seal hash", w.next.parentSealHash,
				"parent seal hash of chain head", chainSealHash,
				"pre-build parent hash", w.next.header.ParentHash,
				"head hash", chainHead.Hash(),
				"head number", chainHead.Number)
			w.next.discard()
			w.next = nil
		}
	}

	// Also discard w.current if it no longer extends the canonical chain.
	// This covers reorgs at N-1 or deeper.
	if w.current != nil && w.current.header.ParentHash != chainHead.Hash() && !useNext {
		log.Debug("Current work is stale due to reorg, discarding",
			"current.parent", w.current.header.ParentHash,
			"head hash", chainHead.Hash(),
			"head number", chainHead.Number)
		w.current.discard()
		w.current = nil
	}

	if useNext {
		log.Info("Block pipeline hit: promoting pre-executed work",
			"block", w.next.header.Number, "parent", w.next.header.ParentHash, "parent seal hash", w.next.parentSealHash, "head", chainHead.Number)
		w.commit(w.next, chainHead, w.fullTaskHook, true, start)
		w.current = w.next
		w.next = nil
		w.mu.Unlock()
	} else {
		w.mu.Unlock()
		log.Debug("Block pipeline miss: building work from chain head",
			"head", chainHead.Number)
		work, err := w.newWork(interrupt, timestamp)
		if err != nil {
			log.Error("Failed to build new work from chain head", "err", err)
			return
		}
		w.mu.Lock()
		// Verify chain head hasn't moved again while we were executing.
		if w.chain.CurrentBlock().Hash() != chainHead.Hash() {
			w.mu.Unlock()
			work.discard()
			log.Warn("Chain head moved during work preparation, will retry on next event")
			return
		}
		w.commit(work, chainHead, w.fullTaskHook, true, start)
		w.current = work
		w.mu.Unlock()
	}
}

// commit runs any post-transaction state modifications, assembles the final block
// and commits new work if consensus engine is running.
// Note the assumption is held that the mutation is allowed to the passed env, do
// the deep copy first.
func (w *worker) commit(env *environment, parent *types.Header, interval func(), update bool, start time.Time) error {
	if w.isRunning() {
		if interval != nil {
			interval()
		}
		// Create a local environment copy, avoid the data race with snapshot state.
		// https://github.com/ethereum/go-ethereum/issues/24299
		env := env.copy()
		// Fix snapshot parent chain: in the pipeline hit path env.state.snap still
		// points to the StateAt root from several blocks ago (copied through the
		// chain task_N-1 → w.next → env). Commit() passes snap.Root() as the
		// parent to snaps.Update, so without this correction every pipeline-hit
		// block registers its snapshot with the wrong parent, causing validators
		// to miss intermediate-block account mutations when traversing the snapshot
		// tree and producing a different state root.
		env.state.ResetSnap(parent.Root)
		// Create and add the WDC reward transaction for PoW 2.0.

		systemTx, err := cpow.CreateWDCMinedTx(w.chainConfig, w.chain.WdcCache, parent.Nonce.Uint64(), parent.Number.Uint64(), env.header.BaseFee)
		if err != nil {
			log.Error("Failed to create WDC reward transaction", "err", err)
			return fmt.Errorf("failed to create WDC reward transaction: %w", err)
		}

		if env.gasPool == nil {
			env.gasPool = new(core.GasPool).AddGas(env.header.GasLimit)
		}

		env.state.SetTxContext(systemTx.Hash(), env.tcount)
		if _, err := w.commitTransaction(env, systemTx); err != nil {
			log.Error("Failed to commit WDC reward transaction", "err", err)
			return fmt.Errorf("failed to commit WDC reward transaction: %w", err)
		}

		// Update block after applied system transaction
		env.header.Bloom = types.CreateBloom(env.receipts)

		// Withdrawals are set to nil here, because this is only called in PoW.
		// Uncles are nil: PoW 2.0 has no uncles.
		block, err := w.engine.FinalizeAndAssemble(w.chain, env.header, env.state, env.txs, nil, env.receipts, nil)
		if err != nil {
			return err
		}

		// If we're post merge, just ignore
		if !w.isTTDReached(block.Header()) {
			// Send the task to taskLoop first so pendingTasks[sealHash] is populated
			// before prepareNextWork's buildProposalForCurrentEnv looks it up.
			select {
			case w.taskCh <- &task{receipts: env.receipts, state: env.state, block: block, createdAt: time.Now(), proposalStop: make(chan struct{}), proposalReady: make(chan struct{})}:
				w.unconfirmed.Shift(block.NumberU64() - 1)

				fees := totalFees(block, env.receipts)
				feesInEther := new(big.Float).Quo(new(big.Float).SetInt(fees), big.NewFloat(params.Ether))
				log.Info("Commit new sealing work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
					"txs", env.tcount,
					"gas", block.GasUsed(), "fees", feesInEther,
					"elapsed", common.PrettyDuration(time.Since(start)))
			case <-w.exitCh:
				log.Info("Worker has exited")
			}
		}
	}
	if update {
		w.updateSnapshot(env)
	}
	return nil
}

// getSealingBlock generates the sealing block based on the given parameters.
// The generation result will be passed back via the given channel no matter
// the generation itself succeeds or not.
func (w *worker) getSealingBlock(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash, withdrawals types.Withdrawals, noTxs bool) (*types.Block, *big.Int, error) {
	req := &getWorkReq{
		params: &generateParams{
			timestamp:   timestamp,
			forceTime:   true,
			parentHash:  parent,
			coinbase:    coinbase,
			random:      random,
			withdrawals: withdrawals,
			noUncle:     true,
			noTxs:       noTxs,
		},
		result: make(chan *newPayloadResult, 1),
	}
	select {
	case w.getWorkCh <- req:
		result := <-req.result
		if result.err != nil {
			return nil, nil, result.err
		}
		return result.block, result.fees, nil
	case <-w.exitCh:
		return nil, nil, errors.New("miner closed")
	}
}

// isTTDReached returns the indicator if the given block has reached the total
// terminal difficulty for The Merge transition.
func (w *worker) isTTDReached(header *types.Header) bool {
	td, ttd := w.chain.GetTd(header.ParentHash, header.Number.Uint64()-1), w.chain.Config().TerminalTotalDifficulty
	return td != nil && ttd != nil && td.Cmp(ttd) >= 0
}

// copyReceipts makes a deep copy of the given receipts.
func copyReceipts(receipts []*types.Receipt) []*types.Receipt {
	result := make([]*types.Receipt, len(receipts))
	for i, l := range receipts {
		cpy := *l
		result[i] = &cpy
	}
	return result
}

// totalFees computes total consumed miner fees in Wei. Block transactions and receipts have to have the same order.
func totalFees(block *types.Block, receipts []*types.Receipt) *big.Int {
	feesWei := new(big.Int)
	for i, tx := range block.Transactions() {
		minerFee, _ := tx.EffectiveGasTip(block.BaseFee())
		feesWei.Add(feesWei, new(big.Int).Mul(new(big.Int).SetUint64(receipts[i].GasUsed), minerFee))
	}
	return feesWei
}

// proposalHashList is a DerivableList over a slice of transaction hashes.
// It lets us compute a Merkle root for an N+2 proposal by hashing only the
// 32-byte tx hashes, without fetching full transaction objects from the pool.
type proposalHashList []common.Hash

func (l proposalHashList) Len() int                           { return len(l) }
func (l proposalHashList) EncodeIndex(i int, w *bytes.Buffer) { w.Write(l[i][:]) }

// signalToErr converts the interruption signal to a concrete error type for return.
// The given signal must be a valid interruption signal.
func signalToErr(signal int32) error {
	switch signal {
	case commitInterruptNewHead:
		return errBlockInterruptedByNewHead
	case commitInterruptResubmit:
		return errBlockInterruptedByRecommit
	case commitInterruptTimeout:
		return errBlockInterruptedByTimeout
	default:
		panic(fmt.Errorf("undefined signal %d", signal))
	}
}
