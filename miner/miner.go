//版权所有2014年作者
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

//包装矿工实施以太坊的创建和采矿。
package miner

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// 后端包装采矿所需的所有方法。只有完整节点才能
//在这里提供所有功能。
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *core.TxPool
}

// 配置是采矿的配置参数。
type Config struct {
	Etherbase  common.Address `toml:",omitempty"` // Public address for block mining rewards (default = first account)
	Notify     []string       `toml:",omitempty"` // HTTP URL list to be notified of new work packages (only useful in ethash).
	NotifyFull bool           `toml:",omitempty"` // Notify with pending block headers instead of work packages
	ExtraData  hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	GasFloor   uint64         // Target gas floor for mined blocks.
	GasCeil    uint64         // Target gas ceiling for mined blocks.
	GasPrice   *big.Int       // Minimum gas price for mining a transaction
	Recommit   time.Duration  // The time interval for miner to re-create mining work.
	Noverify   bool           // Disable remote mining solution verification(only useful in ethash).
}

// 矿工创建块并搜索工作证明值。
type Miner struct {
	mux      *event.TypeMux
	worker   *worker
	coinbase common.Address
	eth      Backend
	engine   consensus.Engine
	exitCh   chan struct{}
	startCh  chan common.Address
	stopCh   chan struct{}

	wg sync.WaitGroup
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(header *types.Header) bool) *Miner {
	miner := &Miner{
		eth:     eth,
		mux:     mux,
		engine:  engine,
		exitCh:  make(chan struct{}),
		startCh: make(chan common.Address),
		stopCh:  make(chan struct{}),
		worker:  newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, true),
	}
	miner.wg.Add(1)
	go miner.update()
	return miner
}

// 更新跟踪下载器事件。请注意，这是一种单镜头的更新循环。
//一旦播放了``完成''或``失败''的事件即将注册，并且
//循环退出。这是为了防止外部各方可以用块的主要安全性vuln
//只要DOS继续前进，就会停止采矿业务。
func (miner *Miner) update() {
	defer miner.wg.Done()

	events := miner.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
	defer func() {
		if !events.Closed() {
			events.Unsubscribe()
		}
	}()

	shouldStart := false
	canStart := true
	dlEventCh := events.Chan()
	for {
		select {
		case ev := <-dlEventCh:
			if ev == nil {
				// Unsubscription done, stop listening
				dlEventCh = nil
				continue
			}
			switch ev.Data.(type) {
			case downloader.StartEvent:
				wasMining := miner.Mining()
				miner.worker.stop()
				canStart = false
				if wasMining {
					// Resume mining after sync was finished
					shouldStart = true
					log.Info("Mining aborted due to sync")
				}
			case downloader.FailedEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
			case downloader.DoneEvent:
				canStart = true
				if shouldStart {
					miner.SetEtherbase(miner.coinbase)
					miner.worker.start()
				}
				// Stop reacting to downloader events
				events.Unsubscribe()
			}
		case addr := <-miner.startCh:
			miner.SetEtherbase(addr)
			if canStart {
				miner.worker.start()
			}
			shouldStart = true
		case <-miner.stopCh:
			shouldStart = false
			miner.worker.stop()
		case <-miner.exitCh:
			miner.worker.close()
			return
		}
	}
}

func (miner *Miner) Start(coinbase common.Address) {
	miner.startCh <- coinbase
}

func (miner *Miner) Stop() {
	miner.stopCh <- struct{}{}
}

func (miner *Miner) Close() {
	close(miner.exitCh)
	miner.wg.Wait()
}

func (miner *Miner) Mining() bool {
	return miner.worker.isRunning()
}

func (miner *Miner) Hashrate() uint64 {
	if pow, ok := miner.engine.(consensus.PoW); ok {
		return uint64(pow.Hashrate())
	}
	return 0
}

func (miner *Miner) SetExtra(extra []byte) error {
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
	}
	miner.worker.setExtra(extra)
	return nil
}

// SetRecommitInterval设置了密封工作重新提交的间隔。
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// 等待返回当前待处理的块和相关状态。
func (miner *Miner) Pending() (*types.Block, *state.StateDB) {
	return miner.worker.pending()
}

// pendendBlock返回当前未决的块。
//
//注意，要访问待处理的块和待处理状态
//同时，请使用repending（），如待处理状态可以
//在多个方法调用之间更改
func (miner *Miner) PendingBlock() *types.Block {
	return miner.worker.pendingBlock()
}

// perdendblockandReceipts返回当前待处理的块和相应的收据。
func (miner *Miner) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return miner.worker.pendingBlockAndReceipts()
}

func (miner *Miner) SetEtherbase(addr common.Address) {
	miner.coinbase = addr
	miner.worker.setEtherbase(addr)
}

//设定的GASCEIL将气体限制设置为1559年后采矿块时的努力。
//对于1559年前的块，它设置了天花板。
func (miner *Miner) SetGasCeil(ceil uint64) {
	miner.worker.setGasCeil(ceil)
}

// 启用PRESEAL打开Peseal采矿功能。默认情况下启用了它。
//注意此功能不应暴露于API，对于用户来说是不必要的
//（矿工）实际了解基础细节。仅适用于外部项目
//使用此库。
func (miner *Miner) EnablePreseal() {
	miner.worker.enablePreseal()
}

// Disable Proseal关闭Prepeal采矿功能。有些人有必要
//假共识引擎，可以立即密封块。
//注意此功能不应暴露于API，对于用户来说是不必要的
//（矿工）实际了解基础细节。仅适用于外部项目
//使用此库。
func (miner *Miner) DisablePreseal() {
	miner.worker.disablePreseal()
}

// SubscribePendingLogs starts delivering logs from pending transactions
// to the given channel.
func (miner *Miner) SubscribePendingLogs(ch chan<- []*types.Log) event.Subscription {
	return miner.worker.pendingLogsFeed.Subscribe(ch)
}

// GetSealingBlockAsync requests to generate a sealing block according to the
// given parameters. Regardless of whether the generation is successful or not,
// there is always a result that will be returned through the result channel.
// The difference is that if the execution fails, the returned result is nil
// and the concrete error is dropped silently.
func (miner *Miner) GetSealingBlockAsync(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash, noTxs bool) (chan *types.Block, error) {
	resCh, _, err := miner.worker.getSealingBlock(parent, timestamp, coinbase, random, noTxs)
	if err != nil {
		return nil, err
	}
	return resCh, nil
}

// GetSealingBlockSync creates a sealing block according to the given parameters.
// If the generation is failed or the underlying work is already closed, an error
// will be returned.
func (miner *Miner) GetSealingBlockSync(parent common.Hash, timestamp uint64, coinbase common.Address, random common.Hash, noTxs bool) (*types.Block, error) {
	resCh, errCh, err := miner.worker.getSealingBlock(parent, timestamp, coinbase, random, noTxs)
	if err != nil {
		return nil, err
	}
	return <-resCh, <-errCh
}
