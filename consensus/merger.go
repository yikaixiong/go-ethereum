//版权2021 The Go-Ethereum作者
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



// TODO:  pow 向 pos  过度
package consensus

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)
// TransitionStatus描述了ETH1/2过渡的状态。这个开关
//模式之间的单向操作是通过相应的
//共识邮件。
type transitionStatus struct {
	LeftPoW    bool // The flag is set when the first NewHead message received
	EnteredPoS bool // The flag is set when the first FinalisedBlock message received
}

//合并是用于跟踪ETH1/2过渡状态的内部帮助结构。
//这是一个共同的结构，可以在完整的节点和轻度客户端中使用。
type Merger struct {
	db     ethdb.KeyValueStore
	status transitionStatus
	mu     sync.RWMutex
}

// Newmerger创建了新的合并，该合并将其过渡状态存储在提供的DB中。
func NewMerger(db ethdb.KeyValueStore) *Merger {
	var status transitionStatus
	blob := rawdb.ReadTransitionStatus(db)
	if len(blob) != 0 {
		if err := rlp.DecodeBytes(blob, &status); err != nil {
			log.Crit("Failed to decode the transition status", "err", err)
		}
	}
	return &Merger{
		db:     db,
		status: status,
	}
}

// 每当收到的第一个新黑德消息
//来自共识层。
func (m *Merger) ReachTTD() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.LeftPoW {
		return
	}
	m.status = transitionStatus{LeftPoW: true}
	blob, err := rlp.EncodeToBytes(m.status)
	if err != nil {
		panic(fmt.Sprintf("Failed to encode the transition status: %v", err))
	}
	rawdb.WriteTransitionStatus(m.db, blob)
	log.Info("Left PoW stage")
}

// 每当收到的第一个finalishisedlock消息时，最终iizepos都会称为
//来自共识层。
func (m *Merger) FinalizePoS() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.EnteredPoS {
		return
	}
	m.status = transitionStatus{LeftPoW: true, EnteredPoS: true}
	blob, err := rlp.EncodeToBytes(m.status)
	if err != nil {
		panic(fmt.Sprintf("Failed to encode the transition status: %v", err))
	}
	rawdb.WriteTransitionStatus(m.db, blob)
	log.Info("Entered PoS stage")
}

// TDDREACH报告了该链是否离开了战俘舞台。
func (m *Merger) TDDReached() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.status.LeftPoW
}

// Posfinalized报告该链是否进入POS阶段。
func (m *Merger) PoSFinalized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.status.EnteredPoS
}
