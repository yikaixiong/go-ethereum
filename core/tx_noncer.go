//版权所有2019年作者
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

package core

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
)

// txnoncer是一个微小的虚拟状态数据库，用于管理
//池中的帐户，如果
//一个帐户未知。
type txNoncer struct {
	fallback *state.StateDB
	nonces   map[common.Address]uint64
	lock     sync.Mutex
}

// newtxnoncer创建了一个新的虚拟状态数据库来跟踪池NONCES。
func newTxNoncer(statedb *state.StateDB) *txNoncer {
	return &txNoncer{
		fallback: statedb.Copy(),
		nonces:   make(map[common.Address]uint64),
	}
}

// get returns the current nonce of an account, falling back to a real state
// database if the account is unknown.
func (txn *txNoncer) get(addr common.Address) uint64 {
	// 我们使用Mutex进行操作是基础
//状态即使以读取访问权限也将突变DB。
	txn.lock.Lock()
	defer txn.lock.Unlock()

	if _, ok := txn.nonces[addr]; !ok {
		txn.nonces[addr] = txn.fallback.GetNonce(addr)
	}
	return txn.nonces[addr]
}

//集合将新的虚拟nonce插入要返回的虚拟状态数据库
//每当池要求它而不是进入真实状态数据库时。
func (txn *txNoncer) set(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()

	txn.nonces[addr] = nonce
}

// setiflower如果是
//新的较低。
func (txn *txNoncer) setIfLower(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()

	if _, ok := txn.nonces[addr]; !ok {
		txn.nonces[addr] = txn.fallback.GetNonce(addr)
	}
	if txn.nonces[addr] <= nonce {
		return
	}
	txn.nonces[addr] = nonce
}

// setAll sets the nonces for all accounts to the given map.
func (txn *txNoncer) setAll(all map[common.Address]uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()

	txn.nonces = all
}
