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

package core

import (
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)
//验证器是一个定义块验证标准的接口。它
//仅负责验证块内容，因为标题验证是
//由特定共识引擎完成。
type Validator interface {
	// 验证机构验证给定块的内容。
	ValidateBody(block *types.Block) error

	// valialateTate验证了给定的陈述b，并选择了收据和
	//使用的气体。
	ValidateState(block *types.Block, state *state.StateDB, receipts types.Receipts, usedGas uint64) error
}

//预摘要是预求职交易签名和状态的接口。
type Prefetcher interface {
	// 预摘要通过运行来根据以太坊规则进行处理。
//使用SETADEDB的事务消息，但任何更改都会丢弃。这
//唯一的目标是预先调查交易签名和状态Trie节点。
	Prefetch(block *types.Block, statedb *state.StateDB, cfg vm.Config, interrupt *uint32)
}

// 处理器是使用给定初始状态处理块的接口。
type Processor interface {
	// 处理过程通过运行根据以太坊规则而变化
//使用陈述的B进行交易消息，并将任何奖励应用于两者
//处理器（Coinbase）和任何包含的叔叔。
	Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error)
}
