//版权所有2017年作者
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

package accounts

import (
	"reflect"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// 管理套件确定了多少个即将到来的钱包事件
// 经理将在其频道中缓冲。
const managerSubBufferSize = 50

// 配置包含全局客户管理器的设置。
//
// TODO（RJL493456442，Karalabe，Holiman）：在帐户管理
// 被删除以支持Clef。
type Config struct {
	InsecureUnlockAllowed bool // 是否允许在不安全的环境中解锁帐户
}

// newbackendevent让经理知道应该
//跟踪给定的后端以进行钱包更新。
type newBackendEvent struct {
	backend   Backend
	processed chan struct{} // 告知事件发射器，后端已被整合
}

// 经理是一位总体客户经理，可以与各种交流
//签署交易的后端。
type Manager struct {
	config      *Config                    // 全局客户经理配置
	backends    map[reflect.Type][]Backend // 当前注册的后端索引
	updaters    []event.Subscription       // 所有后端的钱包更新订阅
	updates     chan WalletEvent           // 后端钱包更改的订阅水槽
	newBackends chan newBackendEvent       // 传入的后端将由经理跟踪
	wallets     []Wallet                   // 所有注册后端的所有钱包的缓存

	feed event.Feed // 钱包饲料通知到达/出发

	quit chan chan error
	term chan struct{} // 终止更新循环后，频道关闭
	lock sync.RWMutex
}

// NewManager创建了一个通用客户经理，可以通过各种
//支持的后端。
func NewManager(config *Config, backends ...Backend) *Manager {
	// 从后端检索钱包的初始清单，然后按URL排序
	var wallets []Wallet
	for _, backend := range backends {
		wallets = merge(wallets, backend.Wallets()...)
	}
	// 订阅所有后端的钱包通知
	updates := make(chan WalletEvent, managerSubBufferSize)

	subs := make([]event.Subscription, len(backends))
	for i, backend := range backends {
		subs[i] = backend.Subscribe(updates)
	}
	// 组装客户经理并退货
	am := &Manager{
		config:      config,
		backends:    make(map[reflect.Type][]Backend),
		updaters:    subs,
		updates:     updates,
		newBackends: make(chan newBackendEvent),
		wallets:     wallets,
		quit:        make(chan chan error),
		term:        make(chan struct{}),
	}
	for _, backend := range backends {
		kind := reflect.TypeOf(backend)
		am.backends[kind] = append(am.backends[kind], backend)
	}
	go am.update()

	return am
}

// 关闭终止客户经理的内部通知流程。
func (am *Manager) Close() error {
	errc := make(chan error)
	am.quit <- errc
	return <-errc
}

// 配置返回帐户管理器的配置。
func (am *Manager) Config() *Config {
	return am.config
}

// 倒带开始跟踪钱包更新的其他后端。
// cmd/geth假设一旦此功能返回后端已经集成了。
func (am *Manager) AddBackend(backend Backend) {
	done := make(chan struct{})
	am.newBackends <- newBackendEvent{backend, done}
	<-done
}

// 更新是Wallet事件循环收听后端的通知
//并更新钱包的缓存。
func (am *Manager) update() {
	// 当经理终止时关闭所有订阅
	defer func() {
		am.lock.Lock()
		for _, sub := range am.updaters {
			sub.Unsubscribe()
		}
		am.updaters = nil
		am.lock.Unlock()
	}()

	// 循环直到终止
	for {
		select {
		case event := <-am.updates:
			// 钱包事件到了，更新本地缓存
			am.lock.Lock()
			switch event.Kind {
			case WalletArrived:
				am.wallets = merge(am.wallets, event.Wallet)
			case WalletDropped:
				am.wallets = drop(am.wallets, event.Wallet)
			}
			am.lock.Unlock()

			// 通知任何听众
			am.feed.Send(event)
		case event := <-am.newBackends:
			am.lock.Lock()
			// 更新缓存
			backend := event.backend
			am.wallets = merge(am.wallets, backend.Wallets()...)
			am.updaters = append(am.updaters, backend.Subscribe(am.updates))
			kind := reflect.TypeOf(backend)
			am.backends[kind] = append(am.backends[kind], backend)
			am.lock.Unlock()
			close(event.processed)
		case errc := <-am.quit:
			// 经理终止，返回
			errc <- nil
			// 信号事件发射器循环未接收值
//防止他们卡住。
			close(am.term)
			return
		}
	}
}

// 后端从客户经理中带有给定类型的后端检索后端。
func (am *Manager) Backends(kind reflect.Type) []Backend {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.backends[kind]
}

//钱包返回该客户经理下注册的所有签名帐户。
func (am *Manager) Wallets() []Wallet {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.walletsNoLock()
}

// Walletsnolock返回所有注册的钱包。呼叫者必须举行am.lock。
func (am *Manager) walletsNoLock() []Wallet {
	cpy := make([]Wallet, len(am.wallets))
	copy(cpy, am.wallets)
	return cpy
}

// 钱包检索与特定URL相关的钱包。
func (am *Manager) Wallet(url string) (Wallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	parsed, err := parseURL(url)
	if err != nil {
		return nil, err
	}
	for _, wallet := range am.walletsNoLock() {
		if wallet.URL() == parsed {
			return wallet, nil
		}
	}
	return nil, ErrUnknownWallet
}

// 帐户返回帐户经理中所有钱包的所有帐户地址
func (am *Manager) Accounts() []common.Address {
	am.lock.RLock()
	defer am.lock.RUnlock()

	addresses := make([]common.Address, 0) // return [] instead of nil if empty
	for _, wallet := range am.wallets {
		for _, account := range wallet.Accounts() {
			addresses = append(addresses, account.Address)
		}
	}
	return addresses
}

// 查找试图找到与特定帐户相对应的钱包。自从
//可以将帐户动态添加到钱包中并从钱包中删除，此方法具有
//钱包数中的线性运行时。
func (am *Manager) Find(account Account) (Wallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	for _, wallet := range am.wallets {
		if wallet.Contains(account) {
			return wallet, nil
		}
	}
	return nil, ErrUnknownAccount
}

// 订阅创建异步订阅，以接收通知
//经理发现钱包从其任何后端的到达或出发。
func (am *Manager) Subscribe(sink chan<- WalletEvent) event.Subscription {
	return am.feed.Subscribe(sink)
}

//合并是对钱包附加的分类类似物，其中的顺序
//通过在正确位置插入新钱包来保存原点列表。
//
//原始切片假定已被URL排序。
func merge(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 })
		if n == len(slice) {
			slice = append(slice, wallet)
			continue
		}
		slice = append(slice[:n], append([]Wallet{wallet}, slice[n:]...)...)
	}
	return slice
}

// 下降是合并的对应物，它从分类中抬起钱包
//缓存并删除指定的。
func drop(slice []Wallet, wallets ...Wallet) []Wallet {
	for _, wallet := range wallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(wallet.URL()) >= 0 })
		if n == len(slice) {
			// 找不到钱包，可能在启动期间发生
			continue
		}
		slice = append(slice[:n], slice[n+1:]...)
	}
	return slice
}
