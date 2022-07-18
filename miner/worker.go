//版权所有2015年作者
//此文件是Go-Ethereum库的一部分。
//
// Go-Ethereum库是免费软件：您可以重新分发它和/或修改
//根据GNU较少的通用公共许可条款的条款，
//免费软件基金会（许可证的3版本）或
//（根据您的选择）任何以后的版本。
//
// go-ethereum库是为了希望它有用，
//但没有任何保修；甚至没有暗示的保证
//适合或适合特定目的的健身。看到
// GNU较少的通用公共许可证以获取更多详细信息。
//
//您应该收到GNU较少的通用公共许可证的副本
//与Go-Ethereum库一起。如果不是，请参见<http://www.gnu.org/licenses/>。
package miner

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	// 结果是聆听密封结果的通道的大小。
	resultQueueSize = 10

	// TxChansize是聆听newtxsevent的频道的大小。
//该数字是从TX池的大小中引用的。
	txChanSize = 4096

	// ChainheadChansize是聆听Chainheadevent的频道的大小。
	chainHeadChanSize = 10

	// 链式技术是频道聆听幻影的大小。
	chainSideChanSize = 10

	// 重新提交的是重新提交间隔调节通道的大小。
	resubmitAdjustChanSize = 10

	//SealingLogatDepth是在记录成功密封之前的确认数量。
	sealingLogAtDepth = 7

	// MinRecommitInterval是重现密封块的最小时间间隔
//任何新来的交易。
	minRecommitInterval = 1 * time.Second

	// maxRecommitInterval is the maximum time interval to recreate the sealing block with
	// any newly arrived transactions.
	maxRecommitInterval = 15 * time.Second

	// 间隔justratio是单个间隔调整对密封工作的影响
//重新提交间隔。
	intervalAdjustRatio = 0.1

	// 在新的重新提交间隔计算期间应用间隔justbias，有利于
//增加上限或减小下限，以便可以达到极限。
	intervalAdjustBias = 200 * 1000.0 * 1000.0

	// 斯塔特雷斯霍尔德是可接受的陈旧块的最大深度。
	staleThreshold = 7
)

var (
	errBlockInterruptedByNewHead  = errors.New("new head arrived while building block")
	errBlockInterruptedByRecommit = errors.New("recommit interrupt while building block")
)

// 环境是工人当前的环境，并持有一切
//密封块生成的信息。
type environment struct {
	signer types.Signer

	state     *state.StateDB // apply state changes here
	ancestors mapset.Set     // ancestor set (used for checking uncle parent validity)
	family    mapset.Set     // family set (used for checking uncle invalidity)
	tcount    int            // tx count in cycle
	gasPool   *core.GasPool  // available gas used to pack transactions
	coinbase  common.Address

	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
	uncles   map[common.Hash]*types.Header
}

// 复制创建了深度的环境副本。
func (env *environment) copy() *environment {
	cpy := &environment{
		signer:    env.signer,
		state:     env.state.Copy(),
		ancestors: env.ancestors.Clone(),
		family:    env.family.Clone(),
		tcount:    env.tcount,
		coinbase:  env.coinbase,
		header:    types.CopyHeader(env.header),
		receipts:  copyReceipts(env.receipts),
	}
	if env.gasPool != nil {
		gasPool := *env.gasPool
		cpy.gasPool = &gasPool
	}
	// TXS和叔叔的内容是不变的，不必要的
//为他们做昂贵的深副本。
	cpy.txs = make([]*types.Transaction, len(env.txs))
	copy(cpy.txs, env.txs)
	cpy.uncles = make(map[common.Hash]*types.Header)
	for hash, uncle := range env.uncles {
		cpy.uncles[hash] = uncle
	}
	return cpy
}

// Unclelist将包含的叔叔返回列表格式。
func (env *environment) unclelist() []*types.Header {
	var uncles []*types.Header
	for _, uncle := range env.uncles {
		uncles = append(uncles, uncle)
	}
	return uncles
}

// 丢弃终止背景预摘要go-Rutine。它应该
//始终为所有创建的环境实例呼叫
//可能发生Go-Routine泄漏。
func (env *environment) discard() {
	if env.state == nil {
		return
	}
	env.state.StopPrefetcher()
}

// 任务包含所有有关共识引擎密封和结果提交的信息。
type task struct {
	receipts  []*types.Receipt
	state     *state.StateDB
	block     *types.Block
	createdAt time.Time
}

const (
	commitInterruptNone int32 = iota
	commitInterruptNewHead
	commitInterruptResubmit
)

// NewworkReq代表了提交相对中断通知器的新密封工作的请求。
type newWorkReq struct {
	interrupt *int32
	noempty   bool
	timestamp int64
}

// getWorkReq represents a request for getting a new sealing work with provided parameters.
type getWorkReq struct {
	params *generateParams
	result chan *types.Block // non-blocking channel
	err    chan error
}

// 间隔表示重新提交间隔调整。
type intervalAdjust struct {
	ratio float64
	inc   bool
}

// 工人是负责提交新作品的主要对象
//并收集密封结果。
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
	chainSideCh  chan core.ChainSideEvent
	chainSideSub event.Subscription

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

	current      *environment                 // An environment for current running cycle.
	localUncles  map[common.Hash]*types.Block // A set of side blocks generated locally as the possible uncle blocks.
	remoteUncles map[common.Hash]*types.Block // A set of side blocks as the possible uncle blocks.
	unconfirmed  *unconfirmedBlocks           // A set of locally mined blocks pending canonicalness confirmations.

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
	running int32 // The indicator whether the consensus engine is running or not.
	newTxs  int32 // New arrival transaction count since last sealing work submitting.

	// noempty is the flag used to control whether the feature of pre-seal empty
	// block is enabled. The default value is false(pre-seal is enabled by default).
	// But in some special scenario the consensus engine will seal blocks instantaneously,
	// in this case this feature will add all empty blocks into canonical chain
	// non-stop and no real transaction will be included.
	noempty uint32

	// External functions
	isLocalBlock func(header *types.Header) bool // Function used to determine whether the specified block is mined by local miner.

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
		mux:                mux,
		chain:              eth.BlockChain(),
		isLocalBlock:       isLocalBlock,
		localUncles:        make(map[common.Hash]*types.Block),
		remoteUncles:       make(map[common.Hash]*types.Block),
		unconfirmed:        newUnconfirmedBlocks(eth.BlockChain(), sealingLogAtDepth),
		pendingTasks:       make(map[common.Hash]*task),
		txsCh:              make(chan core.NewTxsEvent, txChanSize),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		chainSideCh:        make(chan core.ChainSideEvent, chainSideChanSize),
		newWorkCh:          make(chan *newWorkReq),
		getWorkCh:          make(chan *getWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		exitCh:             make(chan struct{}),
		startCh:            make(chan struct{}, 1),
		resubmitIntervalCh: make(chan time.Duration),
		resubmitAdjustCh:   make(chan *intervalAdjust, resubmitAdjustChanSize),
	}
	// 订阅TX池的NEWTXSEVENT
	worker.txsSub = eth.TxPool().SubscribeNewTxsEvent(worker.txsCh)
	// 订阅区块链事件
	worker.chainHeadSub = eth.BlockChain().SubscribeChainHeadEvent(worker.chainHeadCh)
	worker.chainSideSub = eth.BlockChain().SubscribeChainSideEvent(worker.chainSideCh)

	// 如果用户指定的时间太短，则消毒推荐间隔。
	recommit := worker.config.Recommit
	if recommit < minRecommitInterval {
		log.Warn("Sanitizing miner recommit interval", "provided", recommit, "updated", minRecommitInterval)
		recommit = minRecommitInterval
	}

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

//setetherbase设置用于初始化块共插基键字段的醚键。
func (w *worker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

func (w *worker) setGasCeil(ceil uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.GasCeil = ceil
}

// setExtra设置用于初始化块额外字段的内容。
func (w *worker) setExtra(extra []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.extra = extra
}

//SetRecommitInterval更新了矿工密封工作的间隔。
func (w *worker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

//禁用销售禁用预密封功能
func (w *worker) disablePreseal() {
	atomic.StoreUint32(&w.noempty, 1)
}

// 启用PRESEAL启用预密封功能
func (w *worker) enablePreseal() {
	atomic.StoreUint32(&w.noempty, 0)
}

//待处理返回待处理状态和相应的块。
func (w *worker) pending() (*types.Block, *state.StateDB) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendendBlock返回待处理块。
func (w *worker) pendingBlock() *types.Block {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// 待blockandReceipts返回待处理的块和相应的收据。
func (w *worker) pendingBlockAndReceipts() (*types.Block, types.Receipts) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock, w.snapshotReceipts
}

// Start将运行状态设置为1，并触发提交的新工作。
func (w *worker) start() {
	atomic.StoreInt32(&w.running, 1)
	w.startCh <- struct{}{}
}

// 停止将运行状态设置为0。
func (w *worker) stop() {
	atomic.StoreInt32(&w.running, 0)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return atomic.LoadInt32(&w.running) == 1
}

// close terminates all background threads maintained by the worker.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	atomic.StoreInt32(&w.running, 0)
	close(w.exitCh)
	w.wg.Wait()
}

// 重新兑现反馈时重新计算重新提交间隔。
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

// Newworkloop是独立的Goroutine，可以在收到的活动中提交新的密封工作。
func (w *worker) newWorkLoop(recommit time.Duration) {
	defer w.wg.Done()
	var (
		interrupt   *int32
		minRecommit = recommit // minimal resubmit interval specified by user.
		timestamp   int64      // timestamp for each round of sealing.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(noempty bool, s int32) {
		if interrupt != nil {
			atomic.StoreInt32(interrupt, s)
		}
		interrupt = new(int32)
		select {
		case w.newWorkCh <- &newWorkReq{interrupt: interrupt, noempty: noempty, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
		atomic.StoreInt32(&w.newTxs, 0)
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
			clearPending(w.chain.CurrentBlock().NumberU64())
			timestamp = time.Now().Unix()
			commit(false, commitInterruptNewHead)

		case head := <-w.chainHeadCh:
			clearPending(head.Block.NumberU64())
			timestamp = time.Now().Unix()
			commit(false, commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() && (w.chainConfig.Clique == nil || w.chainConfig.Clique.Period > 0) {
				// Short circuit if no new transaction arrives.
				if atomic.LoadInt32(&w.newTxs) == 0 {
					timer.Reset(recommit)
					continue
				}
				commit(true, commitInterruptResubmit)
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

// Mainloop负责基于
//收到的活动。它可以支持两种模式：自动生成任务和
//根据给定的参数提交或返回任务，以供各种建议。
func (w *worker) mainLoop() {
	defer w.wg.Done()
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()
	defer func() {
		if w.current != nil {
			w.current.discard()
		}
	}()

	cleanTicker := time.NewTicker(time.Second * 10)
	defer cleanTicker.Stop()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interrupt, req.noempty, req.timestamp)

		case req := <-w.getWorkCh:
			block, err := w.generateWork(req.params)
			if err != nil {
				req.err <- err
				req.result <- nil
			} else {
				req.err <- nil
				req.result <- block
			}
		case ev := <-w.chainSideCh:
			// Short circuit for duplicate side blocks
			if _, exist := w.localUncles[ev.Block.Hash()]; exist {
				continue
			}
			if _, exist := w.remoteUncles[ev.Block.Hash()]; exist {
				continue
			}
			// Add side block to possible uncle block set depending on the author.
			if w.isLocalBlock != nil && w.isLocalBlock(ev.Block.Header()) {
				w.localUncles[ev.Block.Hash()] = ev.Block
			} else {
				w.remoteUncles[ev.Block.Hash()] = ev.Block
			}
			// If our sealing block contains less than 2 uncle blocks,
			// add the new uncle block if valid and regenerate a new
			// sealing block for higher profit.
			if w.isRunning() && w.current != nil && len(w.current.uncles) < 2 {
				start := time.Now()
				if err := w.commitUncle(w.current, ev.Block.Header()); err == nil {
					w.commit(w.current.copy(), nil, true, start)
				}
			}

		case <-cleanTicker.C:
			chainHead := w.chain.CurrentBlock()
			for hash, uncle := range w.localUncles {
				if uncle.NumberU64()+staleThreshold <= chainHead.NumberU64() {
					delete(w.localUncles, hash)
				}
			}
			for hash, uncle := range w.remoteUncles {
				if uncle.NumberU64()+staleThreshold <= chainHead.NumberU64() {
					delete(w.remoteUncles, hash)
				}
			}

		case ev := <-w.txsCh:
			// Apply transactions to the pending state if we're not sealing
			//
			// Note all transactions received may not be continuous with transactions
			// already included in the current sealing block. These transactions will
			// be automatically eliminated.
			if !w.isRunning() && w.current != nil {
				// If block is already full, abort
				if gp := w.current.gasPool; gp != nil && gp.Gas() < params.TxGas {
					continue
				}
				txs := make(map[common.Address]types.Transactions)
				for _, tx := range ev.Txs {
					acc, _ := types.Sender(w.current.signer, tx)
					txs[acc] = append(txs[acc], tx)
				}
				txset := types.NewTransactionsByPriceAndNonce(w.current.signer, txs, w.current.header.BaseFee)
				tcount := w.current.tcount
				w.commitTransactions(w.current, txset, nil)

				// Only update the snapshot if any new transactions were added
				// to the pending block
				if tcount != w.current.tcount {
					w.updateSnapshot(w.current)
				}
			} else {
				// Special case, if the consensus engine is 0 period clique(dev mode),
				// submit sealing work here since all empty submission will be rejected
				// by clique. Of course the advance sealing(empty submission) is disabled.
				if w.chainConfig.Clique != nil && w.chainConfig.Clique.Period == 0 {
					w.commitWork(nil, true, time.Now().Unix())
				}
			}
			atomic.AddInt32(&w.newTxs, int32(len(ev.Txs)))

		// System stopped
		case <-w.exitCh:
			return
		case <-w.txsSub.Err():
			return
		case <-w.chainHeadSub.Err():
			return
		case <-w.chainSideSub.Err():
			return
		}
	}
}

// Taskloop是一个独立的Goroutine，可以从发电机和
//将它们推到共识引擎。
func (w *worker) taskLoop() {
	defer w.wg.Done()
	var (
		stopCh chan struct{}
		prev   common.Hash
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
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
			stopCh, prev = make(chan struct{}), sealHash

			if w.skipSealHook != nil && w.skipSealHook(task) {
				continue
			}
			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = task
			w.pendingMu.Unlock()

			if err := w.engine.Seal(w.chain, task.block, w.resultCh, stopCh); err != nil {
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

// Resultloop是独立的goroutine，可以处理密封结果提交
//和数据库的齐平相对数据。
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
				sealhash = w.engine.SealHash(block.Header())
				hash     = block.Hash()
			)
			w.pendingMu.RLock()
			task, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", hash)
				continue
			}
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
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", hash,
				"elapsed", common.PrettyDuration(time.Since(task.createdAt)))

			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

			// Insert the block into the set of pending ones to resultLoop for confirmations
			w.unconfirmed.Insert(block.NumberU64(), block.Hash())

		case <-w.exitCh:
			return
		}
	}
}

// Makeenv为密封块创造了一个新的环境。
func (w *worker) makeEnv(parent *types.Block, header *types.Header, coinbase common.Address) (*environment, error) {
	// Retrieve the parent state to execute on top and start a prefetcher for
	// the miner to speed block sealing up a bit.
	state, err := w.chain.StateAt(parent.Root())
	if err != nil {
		return nil, err
	}
	state.StartPrefetcher("miner")

	// Note the passed coinbase may be different with header.Coinbase.
	env := &environment{
		signer:    types.MakeSigner(w.chainConfig, header.Number),
		state:     state,
		coinbase:  coinbase,
		ancestors: mapset.NewSet(),
		family:    mapset.NewSet(),
		header:    header,
		uncles:    make(map[common.Hash]*types.Header),
	}
	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range w.chain.GetBlocksFromHash(parent.Hash(), 7) {
		for _, uncle := range ancestor.Uncles() {
			env.family.Add(uncle.Hash())
		}
		env.family.Add(ancestor.Hash())
		env.ancestors.Add(ancestor.Hash())
	}
	// Keep track of transactions which return errors so they can be removed
	env.tcount = 0
	return env, nil
}

// commituncle将给定的块添加到叔叔块集中，如果无法添加，则返回错误。
func (w *worker) commitUncle(env *environment, uncle *types.Header) error {
	if w.isTTDReached(env.header) {
		return errors.New("ignore uncle for beacon block")
	}
	hash := uncle.Hash()
	if _, exist := env.uncles[hash]; exist {
		return errors.New("uncle not unique")
	}
	if env.header.ParentHash == uncle.ParentHash {
		return errors.New("uncle is sibling")
	}
	if !env.ancestors.Contains(uncle.ParentHash) {
		return errors.New("uncle's parent unknown")
	}
	if env.family.Contains(hash) {
		return errors.New("uncle already included")
	}
	env.uncles[hash] = uncle
	return nil
}

//更新napshot更新挂起快照块，收据和状态。
func (w *worker) updateSnapshot(env *environment) {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	w.snapshotBlock = types.NewBlock(
		env.header,
		env.txs,
		env.unclelist(),
		env.receipts,
		trie.NewStackTrie(nil),
	)
	w.snapshotReceipts = copyReceipts(env.receipts)
	w.snapshotState = env.state.Copy()
}

func (w *worker) commitTransaction(env *environment, tx *types.Transaction) ([]*types.Log, error) {
	snap := env.state.Snapshot()

	receipt, err := core.ApplyTransaction(w.chainConfig, w.chain, &env.coinbase, env.gasPool, env.state, env.header, tx, &env.header.GasUsed, *w.chain.GetVMConfig())
	if err != nil {
		env.state.RevertToSnapshot(snap)
		return nil, err
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) commitTransactions(env *environment, txs *types.TransactionsByPriceAndNonce, interrupt *int32) error {
	gasLimit := env.header.GasLimit
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(gasLimit)
	}
	var coalescedLogs []*types.Log

	for {
		// In the following three cases, we will interrupt the execution of the transaction.
		// (1) new head block event arrival, the interrupt signal is 1
		// (2) worker start or restart, the interrupt signal is 1
		// (3) worker recreate the sealing block with any newly arrived transactions, the interrupt signal is 2.
		// For the first two cases, the semi-finished work will be discarded.
		// For the third case, the semi-finished work will be submitted to the consensus engine.
		if interrupt != nil && atomic.LoadInt32(interrupt) != commitInterruptNone {
			// Notify resubmit loop to increase resubmitting interval due to too frequent commits.
			if atomic.LoadInt32(interrupt) == commitInterruptResubmit {
				ratio := float64(gasLimit-env.gasPool.Gas()) / float64(gasLimit)
				if ratio < 0.1 {
					ratio = 0.1
				}
				w.resubmitAdjustCh <- &intervalAdjust{
					ratio: ratio,
					inc:   true,
				}
				return errBlockInterruptedByRecommit
			}
			return errBlockInterruptedByNewHead
		}
		// If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}
		// Retrieve the next transaction and abort if all done
		tx := txs.Peek()
		if tx == nil {
			break
		}
		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		from, _ := types.Sender(env.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		if tx.Protected() && !w.chainConfig.IsEIP155(env.header.Number) {
			log.Trace("Ignoring reply protected transaction", "hash", tx.Hash(), "eip155", w.chainConfig.EIP155Block)

			txs.Pop()
			continue
		}
		// Start executing the transaction
		env.state.Prepare(tx.Hash(), env.tcount)

		logs, err := w.commitTransaction(env, tx)
		switch {
		case errors.Is(err, core.ErrGasLimitReached):
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
			txs.Pop()

		case errors.Is(err, core.ErrNonceTooLow):
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			txs.Shift()

		case errors.Is(err, core.ErrNonceTooHigh):
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			txs.Pop()

		case errors.Is(err, nil):
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			txs.Shift()

		case errors.Is(err, core.ErrTxTypeNotSupported):
			// Pop the unsupported transaction without shifting in the next from the account
			log.Trace("Skipping unsupported transaction type", "sender", from, "type", tx.Type())
			txs.Pop()

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()
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
	// Notify resubmit loop to decrease resubmitting interval if current interval is larger
	// than the user-specified one.
	if interrupt != nil {
		w.resubmitAdjustCh <- &intervalAdjust{inc: false}
	}
	return nil
}

// generateParams wraps various of settings for generating sealing task.
type generateParams struct {
	timestamp  uint64         // The timstamp for sealing task
	forceTime  bool           // Flag whether the given timestamp is immutable or not
	parentHash common.Hash    // Parent block hash, empty means the latest chain head
	coinbase   common.Address // The fee recipient address for including transaction
	random     common.Hash    // The randomness generated by beacon chain, empty before the merge
	noUncle    bool           // Flag whether the uncle block inclusion is allowed
	noExtra    bool           // Flag whether the extra field assignment is allowed
	noTxs      bool           // Flag whether an empty block without any transaction is expected
}

// 准备工作根据给定参数构造密封任务，
//基于最后一个链头或指定的父。在此功能中
//尚未填写待处理的交易，仅返回空的任务。
func (w *worker) prepareWork(genParams *generateParams) (*environment, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Find the parent block for sealing task
	parent := w.chain.CurrentBlock()
	if genParams.parentHash != (common.Hash{}) {
		parent = w.chain.GetBlockByHash(genParams.parentHash)
	}
	if parent == nil {
		return nil, fmt.Errorf("missing parent")
	}
	// Sanity check the timestamp correctness, recap the timestamp
	// to parent+1 if the mutation is allowed.
	timestamp := genParams.timestamp
	if parent.Time() >= timestamp {
		if genParams.forceTime {
			return nil, fmt.Errorf("invalid timestamp, parent %d given %d", parent.Time(), timestamp)
		}
		timestamp = parent.Time() + 1
	}
	// Construct the sealing block header, set the extra field if it's allowed
	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		GasLimit:   core.CalcGasLimit(parent.GasLimit(), w.config.GasCeil),
		Time:       timestamp,
		Coinbase:   genParams.coinbase,
	}
	if !genParams.noExtra && len(w.extra) != 0 {
		header.Extra = w.extra
	}
	// Set the randomness field from the beacon chain if it's available.
	if genParams.random != (common.Hash{}) {
		header.MixDigest = genParams.random
	}
	// Set baseFee and GasLimit if we are on an EIP-1559 chain
	if w.chainConfig.IsLondon(header.Number) {
		header.BaseFee = misc.CalcBaseFee(w.chainConfig, parent.Header())
		if !w.chainConfig.IsLondon(parent.Number()) {
			parentGasLimit := parent.GasLimit() * params.ElasticityMultiplier
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
	env, err := w.makeEnv(parent, header, genParams.coinbase)
	if err != nil {
		log.Error("Failed to create sealing context", "err", err)
		return nil, err
	}
	// Accumulate the uncles for the sealing work only if it's allowed.
	if !genParams.noUncle {
		commitUncles := func(blocks map[common.Hash]*types.Block) {
			for hash, uncle := range blocks {
				if len(env.uncles) == 2 {
					break
				}
				if err := w.commitUncle(env, uncle.Header()); err != nil {
					log.Trace("Possible uncle rejected", "hash", hash, "reason", err)
				} else {
					log.Debug("Committing new uncle to block", "hash", hash)
				}
			}
		}
		// Prefer to locally generated uncle
		commitUncles(w.localUncles)
		commitUncles(w.remoteUncles)
	}
	return env, nil
}

// Filltransactions从TXPOOL中检索待处理的交易，并填充它们
//进入给定的密封块。交易选择和订购策略可以
//将来使用插件定制。
func (w *worker) fillTransactions(interrupt *int32, env *environment) error {
	// Split the pending transactions into locals and remotes
	// Fill the block with all available pending transactions.
	pending := w.eth.TxPool().Pending(true)
	localTxs, remoteTxs := make(map[common.Address]types.Transactions), pending
	for _, account := range w.eth.TxPool().Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}
	if len(localTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, localTxs, env.header.BaseFee)
		if err := w.commitTransactions(env, txs, interrupt); err != nil {
			return err
		}
	}
	if len(remoteTxs) > 0 {
		txs := types.NewTransactionsByPriceAndNonce(env.signer, remoteTxs, env.header.BaseFee)
		if err := w.commitTransactions(env, txs, interrupt); err != nil {
			return err
		}
	}
	return nil
}

// 生成工程基于给定参数生成密封块。
func (w *worker) generateWork(params *generateParams) (*types.Block, error) {
	work, err := w.prepareWork(params)
	if err != nil {
		return nil, err
	}
	defer work.discard()

	if !params.noTxs {
		w.fillTransactions(nil, work)
	}
	return w.engine.FinalizeAndAssemble(w.chain, work.header, work.state, work.txs, work.unclelist(), work.receipts)
}

// Commitwork基于父块生成了几个新的密封任务
//并将其提交给密封剂。
func (w *worker) commitWork(interrupt *int32, noempty bool, timestamp int64) {
	start := time.Now()

	// Set the coinbase if the worker is running or it's required
	var coinbase common.Address
	if w.isRunning() {
		if w.coinbase == (common.Address{}) {
			log.Error("Refusing to mine without etherbase")
			return
		}
		coinbase = w.coinbase // Use the preset address as the fee recipient
	}
	work, err := w.prepareWork(&generateParams{
		timestamp: uint64(timestamp),
		coinbase:  coinbase,
	})
	if err != nil {
		return
	}
	// Create an empty block based on temporary copied state for
	// sealing in advance without waiting block execution finished.
	if !noempty && atomic.LoadUint32(&w.noempty) == 0 {
		w.commit(work.copy(), nil, false, start)
	}

	// Fill pending transactions from the txpool
	err = w.fillTransactions(interrupt, work)
	if errors.Is(err, errBlockInterruptedByNewHead) {
		work.discard()
		return
	}
	w.commit(work.copy(), w.fullTaskHook, true, start)

	// Swap out the old work with the new one, terminating any leftover
	// prefetcher processes in the mean time and starting a new one.
	if w.current != nil {
		w.current.discard()
	}
	w.current = work
}

// 提交运行任何交易后状态修改，组装最终块
//如果共识引擎正在运行，则进行新作品。
//注意认为该突变被允许传递给Env，DO
//先副本。
func (w *worker) commit(env *environment, interval func(), update bool, start time.Time) error {
	if w.isRunning() {
		if interval != nil {
			interval()
		}
		// Create a local environment copy, avoid the data race with snapshot state.
		// https://github.com/ethereum/go-ethereum/issues/24299
		env := env.copy()
		block, err := w.engine.FinalizeAndAssemble(w.chain, env.header, env.state, env.txs, env.unclelist(), env.receipts)
		if err != nil {
			return err
		}
		// If we're post merge, just ignore
		if !w.isTTDReached(block.Header()) {
			select {
			case w.taskCh <- &task{receipts: env.receipts, state: env.state, block: block, createdAt: time.Now()}:
				w.unconfirmed.Shift(block.NumberU64() - 1)
				log.Info("Commit new sealing work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
					"uncles", len(env.uncles), "txs", env.tcount,
					"gas", block.GasUsed(), "fees", totalFees(block, env.receipts),
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

// geteAlingBlock根据给定参数生成密封块。
//生成结果将通过给定频道传递给
//一代人本身成功。
func (w *worker) getSealingBlock(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash, noTxs bool) (chan *types.Block, chan error, error) {
	var (
		resCh = make(chan *types.Block, 1)
		errCh = make(chan error, 1)
	)
	req := &getWorkReq{
		params: &generateParams{
			timestamp:  timestamp,
			forceTime:  true,
			parentHash: parent,
			coinbase:   coinbase,
			random:     random,
			noUncle:    true,
			noExtra:    true,
			noTxs:      noTxs,
		},
		result: resCh,
		err:    errCh,
	}
	select {
	case w.getWorkCh <- req:
		return resCh, errCh, nil
	case <-w.exitCh:
		return nil, nil, errors.New("miner closed")
	}
}

// 如果给定的块达到总数
//合并过渡的终端难度。
func (w *worker) isTTDReached(header *types.Header) bool {
	td, ttd := w.chain.GetTd(header.ParentHash, header.Number.Uint64()-1), w.chain.Config().TerminalTotalDifficulty
	return td != nil && ttd != nil && td.Cmp(ttd) >= 0
}

// 版权所有的给定收据的深层副本。
func copyReceipts(receipts []*types.Receipt) []*types.Receipt {
	result := make([]*types.Receipt, len(receipts))
	for i, l := range receipts {
		cpy := *l
		result[i] = &cpy
	}
	return result
}

// postSideBlock fires a side chain event, only use it for testing.
func (w *worker) postSideBlock(event core.ChainSideEvent) {
	select {
	case w.chainSideCh <- event:
	case <-w.exitCh:
	}
}

// TotalFees计算ETH中的总消费矿工费用。块交易和收据必须具有相同的订单。
func totalFees(block *types.Block, receipts []*types.Receipt) *big.Float {
	feesWei := new(big.Int)
	for i, tx := range block.Transactions() {
		minerFee, _ := tx.EffectiveGasTip(block.BaseFee())
		feesWei.Add(feesWei, new(big.Int).Mul(new(big.Int).SetUint64(receipts[i].GasUsed), minerFee))
	}
	return new(big.Float).Quo(new(big.Float).SetInt(feesWei), new(big.Float).SetInt(big.NewInt(params.Ether)))
}
