//版权所有2016年作者
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

package light

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10
)

// Txpermanent是开采交易后的开采块数量
//被认为是永久的，预计不会回滚
var txPermanent = uint64(500)

// TXPOOL实现了轻型客户的交易池，这保持了跟踪
//本地创建的交易状态的状态，检测是否包括
//在街区（开采）或向后滚动。没有排队的交易，因为我们
//始终以与原样的顺序接收所有本地签名的交易
//创建。
type TxPool struct {
	config       *params.ChainConfig
	signer       types.Signer
	quit         chan bool
	txFeed       event.Feed
	scope        event.SubscriptionScope
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription
	mu           sync.RWMutex
	chain        *LightChain
	odr          OdrBackend
	chainDb      ethdb.Database
	relay        TxRelayBackend
	head         common.Hash
	nonce        map[common.Address]uint64            // "pending" nonce
	pending      map[common.Hash]*types.Transaction   // pending transactions by tx hash
	mined        map[common.Hash][]*types.Transaction // mined transactions by block hash
	clearIdx     uint64                               // earliest block nr that can contain mined tx info

	istanbul bool // Fork indicator whether we are in the istanbul stage.
	eip2718  bool // Fork indicator whether we are in the eip2718 stage.
}

// TXRELAYBACKEND提供了转发Transacions的机制的接口
//到ETH网络。功能的实现应无障碍。
//
//将后端发送指令转发新交易
// Newhead通过TX池处理后，通知后端关于新的头部，
//包括上次事件以来的开采和回滚交易
//丢弃通知后端有关应丢弃的交易的通知
//因为它们已被重新定位代替或因为已被开采而取代
//很久以前，预计不会回滚
type TxRelayBackend interface {
	Send(txs types.Transactions)
	NewHead(head common.Hash, mined []common.Hash, rollback []common.Hash)
	Discard(hashes []common.Hash)
}

// NewTxPool creates a new light transaction pool
func NewTxPool(config *params.ChainConfig, chain *LightChain, relay TxRelayBackend) *TxPool {
	pool := &TxPool{
		config:      config,
		signer:      types.LatestSigner(config),
		nonce:       make(map[common.Address]uint64),
		pending:     make(map[common.Hash]*types.Transaction),
		mined:       make(map[common.Hash][]*types.Transaction),
		quit:        make(chan bool),
		chainHeadCh: make(chan core.ChainHeadEvent, chainHeadChanSize),
		chain:       chain,
		relay:       relay,
		odr:         chain.Odr(),
		chainDb:     chain.Odr().Database(),
		head:        chain.CurrentHeader().Hash(),
		clearIdx:    chain.CurrentHeader().Number.Uint64(),
	}
	// Subscribe events from blockchain
	pool.chainHeadSub = pool.chain.SubscribeChainHeadEvent(pool.chainHeadCh)
	go pool.eventLoop()

	return pool
}

// Curstrate返回当前标头的光状态
func (pool *TxPool) currentState(ctx context.Context) *state.StateDB {
	return NewState(ctx, pool.chain.CurrentHeader(), pool.odr)
}

// GetNonce返回给定地址的“待处理” nonce。它总是查询
//也属于最新标头的nonce，以便检测另一个
//使用同一密钥的客户端发送交易。
func (pool *TxPool) GetNonce(ctx context.Context, addr common.Address) (uint64, error) {
	state := pool.currentState(ctx)
	nonce := state.GetNonce(addr)
	if state.Error() != nil {
		return 0, state.Error()
	}
	sn, ok := pool.nonce[addr]
	if ok && sn > nonce {
		nonce = sn
	}
	if !ok || sn < nonce {
		pool.nonce[addr] = nonce
	}
	return nonce, nil
}

// TXSTATATECHANGES存储了最近的待处理/开采状态的变化
//交易。真正的手段开采，错误意味着退缩，没有进入意味着没有更改
type txStateChanges map[common.Hash]bool

// SetState将TX的状态设置为最近开采或最近回滚的状态
func (txc txStateChanges) setState(txHash common.Hash, mined bool) {
	val, ent := txc[txHash]
	if ent && (val != mined) {
		delete(txc, txHash)
	} else {
		txc[txHash] = mined
	}
}

// GetLists创建了开采和回滚的列表TX哈希
func (txc txStateChanges) getLists() (mined []common.Hash, rollback []common.Hash) {
	for hash, val := range txc {
		if val {
			mined = append(mined, hash)
		} else {
			rollback = append(rollback, hash)
		}
	}
	return
}

// CheckMinedTXS检查当前未决的交易的新添加的块
//并在必要时标记为开采。它还将块位置存储在DB中
//并将它们添加到接收到的TXSTATATECHANGES地图中。
func (pool *TxPool) checkMinedTxs(ctx context.Context, hash common.Hash, number uint64, txc txStateChanges) error {
	// If no transactions are pending, we don't care about anything
	if len(pool.pending) == 0 {
		return nil
	}
	block, err := GetBlock(ctx, pool.odr, hash, number)
	if err != nil {
		return err
	}
	// Gather all the local transaction mined in this block
	list := pool.mined[hash]
	for _, tx := range block.Transactions() {
		if _, ok := pool.pending[tx.Hash()]; ok {
			list = append(list, tx)
		}
	}
	// If some transactions have been mined, write the needed data to disk and update
	if list != nil {
		// Retrieve all the receipts belonging to this block and write the loopup table
		if _, err := GetBlockReceipts(ctx, pool.odr, hash, number); err != nil { // ODR caches, ignore results
			return err
		}
		rawdb.WriteTxLookupEntriesByBlock(pool.chainDb, block)

		// Update the transaction pool's state
		for _, tx := range list {
			delete(pool.pending, tx.Hash())
			txc.setState(tx.Hash(), true)
		}
		pool.mined[hash] = list
	}
	return nil
}

// RollbackTXS标记了最近滚动后块中包含的交易
//回滚。它还可以删除任何位置查找条目。
func (pool *TxPool) rollbackTxs(hash common.Hash, txc txStateChanges) {
	batch := pool.chainDb.NewBatch()
	if list, ok := pool.mined[hash]; ok {
		for _, tx := range list {
			txHash := tx.Hash()
			rawdb.DeleteTxLookupEntry(batch, txHash)
			pool.pending[txHash] = tx
			txc.setState(txHash, false)
		}
		delete(pool.mined, hash)
	}
	batch.Write()
}

// Reorgonnewhead设置了一个新的头标头，处理（如有必要，请向后回滚）
//自从最后一个已知的头部以来的块，并返回包含的txstatechanges地图
//最近开采和回滚的交易哈希。如果有错误（上下文
//超时）发生在检查新块时，它留下了本地知名的头部
//在最新的检查块中，仍然返回有效的txstatechanges，使其成为
//可能在下一个连锁主事件中继续检查缺失的块
func (pool *TxPool) reorgOnNewHead(ctx context.Context, newHeader *types.Header) (txStateChanges, error) {
	txc := make(txStateChanges)
	oldh := pool.chain.GetHeaderByHash(pool.head)
	newh := newHeader
	// find common ancestor, create list of rolled back and new block hashes
	var oldHashes, newHashes []common.Hash
	for oldh.Hash() != newh.Hash() {
		if oldh.Number.Uint64() >= newh.Number.Uint64() {
			oldHashes = append(oldHashes, oldh.Hash())
			oldh = pool.chain.GetHeader(oldh.ParentHash, oldh.Number.Uint64()-1)
		}
		if oldh.Number.Uint64() < newh.Number.Uint64() {
			newHashes = append(newHashes, newh.Hash())
			newh = pool.chain.GetHeader(newh.ParentHash, newh.Number.Uint64()-1)
			if newh == nil {
				// happens when CHT syncing, nothing to do
				newh = oldh
			}
		}
	}
	if oldh.Number.Uint64() < pool.clearIdx {
		pool.clearIdx = oldh.Number.Uint64()
	}
	// roll back old blocks
	for _, hash := range oldHashes {
		pool.rollbackTxs(hash, txc)
	}
	pool.head = oldh.Hash()
	// check mined txs of new blocks (array is in reversed order)
	for i := len(newHashes) - 1; i >= 0; i-- {
		hash := newHashes[i]
		if err := pool.checkMinedTxs(ctx, hash, newHeader.Number.Uint64()-uint64(i), txc); err != nil {
			return txc, err
		}
		pool.head = hash
	}

	// clear old mined tx entries of old blocks
	if idx := newHeader.Number.Uint64(); idx > pool.clearIdx+txPermanent {
		idx2 := idx - txPermanent
		if len(pool.mined) > 0 {
			for i := pool.clearIdx; i < idx2; i++ {
				hash := rawdb.ReadCanonicalHash(pool.chainDb, i)
				if list, ok := pool.mined[hash]; ok {
					hashes := make([]common.Hash, len(list))
					for i, tx := range list {
						hashes[i] = tx.Hash()
					}
					pool.relay.Discard(hashes)
					delete(pool.mined, hash)
				}
			}
		}
		pool.clearIdx = idx2
	}

	return txc, nil
}

// BlockCheckTimeOut是检查开采新块的时间限制
//交易。如果时间表的话，请检查下一个连锁主事件的简历。
const blockCheckTimeout = time.Second * 3

// EventLoop处理链头事件，还通知TX继电器后端
//关于新的Head Hash和TX状态更改
func (pool *TxPool) eventLoop() {
	for {
		select {
		case ev := <-pool.chainHeadCh:
			pool.setNewHead(ev.Block.Header())
			// hack in order to avoid hogging the lock; this part will
			// be replaced by a subsequent PR.
			time.Sleep(time.Millisecond)

		// System stopped
		case <-pool.chainHeadSub.Err():
			return
		}
	}
}

func (pool *TxPool) setNewHead(head *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), blockCheckTimeout)
	defer cancel()

	txc, _ := pool.reorgOnNewHead(ctx, head)
	m, r := txc.getLists()
	pool.relay.NewHead(pool.head, m, r)

	// Update fork indicator by next pending block number
	next := new(big.Int).Add(head.Number, big.NewInt(1))
	pool.istanbul = pool.config.IsIstanbul(next)
	pool.eip2718 = pool.config.IsBerlin(next)
}

// Stop stops the light transaction pool
func (pool *TxPool) Stop() {
	// Unsubscribe all subscriptions registered from txpool
	pool.scope.Close()
	// Unsubscribe subscriptions registered from blockchain
	pool.chainHeadSub.Unsubscribe()
	close(pool.quit)
	log.Info("Transaction pool stopped")
}

// SubscribeNewTxsEvent registers a subscription of core.NewTxsEvent and
// starts sending event to the given channel.
func (pool *TxPool) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}

// Stats returns the number of currently pending (locally created) transactions
func (pool *TxPool) Stats() (pending int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	pending = len(pool.pending)
	return
}

// VIALATETX检查事务是否根据共识规则有效。
func (pool *TxPool) validateTx(ctx context.Context, tx *types.Transaction) error {
	// Validate sender
	var (
		from common.Address
		err  error
	)

	// Validate the transaction sender and it's sig. Throw
	// if the from fields is invalid.
	if from, err = types.Sender(pool.signer, tx); err != nil {
		return core.ErrInvalidSender
	}
	// Last but not least check for nonce errors
	currentState := pool.currentState(ctx)
	if n := currentState.GetNonce(from); n > tx.Nonce() {
		return core.ErrNonceTooLow
	}

	// Check the transaction doesn't exceed the current
	// block limit gas.
	header := pool.chain.GetHeaderByHash(pool.head)
	if header.GasLimit < tx.Gas() {
		return core.ErrGasLimit
	}

	// Transactions can't be negative. This may never happen
	// using RLP decoded transactions but may occur if you create
	// a transaction using the RPC for example.
	if tx.Value().Sign() < 0 {
		return core.ErrNegativeValue
	}

	// Transactor should have enough funds to cover the costs
	// cost == V + GP * GL
	if b := currentState.GetBalance(from); b.Cmp(tx.Cost()) < 0 {
		return core.ErrInsufficientFunds
	}

	// Should supply enough intrinsic gas
	gas, err := core.IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, true, pool.istanbul)
	if err != nil {
		return err
	}
	if tx.Gas() < gas {
		return core.ErrIntrinsicGas
	}
	return currentState.Error()
}

// 添加验证新事务，并设置其状态待处理（如果可以处理）。
//如果必要时，它还更新本地存储的nonce。
func (pool *TxPool) add(ctx context.Context, tx *types.Transaction) error {
	hash := tx.Hash()

	if pool.pending[hash] != nil {
		return fmt.Errorf("Known transaction (%x)", hash[:4])
	}
	err := pool.validateTx(ctx, tx)
	if err != nil {
		return err
	}

	if _, ok := pool.pending[hash]; !ok {
		pool.pending[hash] = tx

		nonce := tx.Nonce() + 1

		addr, _ := types.Sender(pool.signer, tx)
		if nonce > pool.nonce[addr] {
			pool.nonce[addr] = nonce
		}

		// Notify the subscribers. This event is posted in a goroutine
		// because it's possible that somewhere during the post "Remove transaction"
		// gets called which will then wait for the global tx pool lock and deadlock.
		go pool.txFeed.Send(core.NewTxsEvent{Txs: types.Transactions{tx}})
	}

	// Print a log message if low enough level is set
	log.Debug("Pooled new transaction", "hash", hash, "from", log.Lazy{Fn: func() common.Address { from, _ := types.Sender(pool.signer, tx); return from }}, "to", tx.To())
	return nil
}

// 如果有效，将添加到池中的交易并将其传递给TX继电器
//后端
func (pool *TxPool) Add(ctx context.Context, tx *types.Transaction) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	data, err := tx.MarshalBinary()
	if err != nil {
		return err
	}

	if err := pool.add(ctx, tx); err != nil {
		return err
	}
	//fmt.Println("Send", tx.Hash())
	pool.relay.Send(types.Transactions{tx})

	pool.chainDb.Put(tx.Hash().Bytes(), data)
	return nil
}

// AddTransactions将所有有效的交易添加到池中，并将其传递给
// TX继电器后端
func (pool *TxPool) AddBatch(ctx context.Context, txs []*types.Transaction) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	var sendTx types.Transactions

	for _, tx := range txs {
		if err := pool.add(ctx, tx); err == nil {
			sendTx = append(sendTx, tx)
		}
	}
	if len(sendTx) > 0 {
		pool.relay.Send(sendTx)
	}
}

// GetTransaction如果包含在池中的交易将返回交易
//否则零。
func (pool *TxPool) GetTransaction(hash common.Hash) *types.Transaction {
	// check the txs first
	if tx, ok := pool.pending[hash]; ok {
		return tx
	}
	return nil
}

// GetTransactions返回当前可处理的所有交易。
//返回的切片可以由呼叫者修改。
func (pool *TxPool) GetTransactions() (txs types.Transactions, err error) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	txs = make(types.Transactions, len(pool.pending))
	i := 0
	for _, tx := range pool.pending {
		txs[i] = tx
		i++
	}
	return txs, nil
}

// 内容检索交易池的数据内容，返回所有
//待定和排队交易，按帐户和nonce分组。
func (pool *TxPool) Content() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// Retrieve all the pending transactions and sort by account and by nonce
	pending := make(map[common.Address]types.Transactions)
	for _, tx := range pool.pending {
		account, _ := types.Sender(pool.signer, tx)
		pending[account] = append(pending[account], tx)
	}
	// There are no queued transactions in a light pool, just return an empty map
	queued := make(map[common.Address]types.Transactions)
	return pending, queued
}

// Contents从检索事务池的数据内容，返回
//待定以及该地址的排队交易，由Nonce分组。
func (pool *TxPool) ContentFrom(addr common.Address) (types.Transactions, types.Transactions) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// Retrieve the pending transactions and sort by nonce
	var pending types.Transactions
	for _, tx := range pool.pending {
		account, _ := types.Sender(pool.signer, tx)
		if account != addr {
			continue
		}
		pending = append(pending, tx)
	}
	// There are no queued transactions in a light pool, just return an empty map
	return pending, types.Transactions{}
}

// RemoveTransactions removes all given transactions from the pool.
func (pool *TxPool) RemoveTransactions(txs types.Transactions) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	var hashes []common.Hash
	batch := pool.chainDb.NewBatch()
	for _, tx := range txs {
		hash := tx.Hash()
		delete(pool.pending, hash)
		batch.Delete(hash.Bytes())
		hashes = append(hashes, hash)
	}
	batch.Write()
	pool.relay.Discard(hashes)
}

// RemoveTx removes the transaction with the given hash from the pool.
func (pool *TxPool) RemoveTx(hash common.Hash) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	// delete from pending pool
	delete(pool.pending, hash)
	pool.chainDb.Delete(hash[:])
	pool.relay.Discard([]common.Hash{hash})
}
