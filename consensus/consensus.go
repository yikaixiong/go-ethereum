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

//包装共识实现不同的以太坊共识引擎。
package consensus

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// ChainheaderReader定义了访问本地所需的少量方法
// 标题验证期间区块链。
type ChainHeaderReader interface {
	// 配置检索区块链的链配置。
	Config() *params.ChainConfig

	// CurrentHeader从本地链中检索当前的标头。
	CurrentHeader() *types.Header

	// Getheader通过哈希和数字从数据库中检索一个块标头。
	GetHeader(hash common.Hash, number uint64) *types.Header

	// GetheaderbyNumber按数字从数据库中检索一个块标头。
	GetHeaderByNumber(number uint64) *types.Header

	// Getheaderbyhash通过其哈希从数据库中检索一个块标头。
	GetHeaderByHash(hash common.Hash) *types.Header

	// GETTD通过哈希和数字从数据库中检索总难度。
	GetTd(hash common.Hash, number uint64) *big.Int
}

// Chainreader定义了访问本地所需的一小部分方法
//标题和/或叔叔验证期间的区块链。
type ChainReader interface {
	ChainHeaderReader

	// getBlock通过哈希和数字从数据库中检索一个块。
	GetBlock(hash common.Hash, number uint64) *types.Block
}

// 发动机是算法不可知的共识引擎。
type Engine interface {
	// 作者检索了铸造给定的帐户的以太坊地址
//块，如果达成共识，这可能与标题的共同基础不同
//引擎基于签名。
	Author(header *types.Header) (common.Address, error)

	// verifyheader检查标头是否符合
	//给定引擎。可以选择地验证密封件，也可以明确完成
	//通过验证方法。
	VerifyHeader(chain ChainHeaderReader, header *types.Header, seal bool) error

	// verifyheaders类似于verifyheader，但验证了一批标题
	//同时。该方法返回退出渠道以中止操作，
	//一个结果渠道检索异步验证（该顺序是
	//输入切片）。
	VerifyHeaders(chain ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error)

	//验证肯定验证给定块的叔叔是否符合共识
	//给定引擎的规则。
	VerifyUncles(chain ChainReader, block *types.Block) error

	// 根据块标头的共识字段的初始化。
	//特定引擎的规则。更改是内联执行的。
	Prepare(chain ChainHeaderReader, header *types.Header) error

	// Finalize runs any post-transaction state modifications (e.g. block rewards)
	// but does not assemble the block.
	//
	// Note: The block header and state database might be updated to reflect any
	// consensus rules that happen at finalization (e.g. block rewards).
	Finalize(chain ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
		uncles []*types.Header)

	// FinalizeAndAssemble runs any post-transaction state modifications (e.g. block
	// rewards) and assembles the final block.
	//
	// Note: The block header and state database might be updated to reflect any
	// consensus rules that happen at finalization (e.g. block rewards).
	FinalizeAndAssemble(chain ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
		uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error)

	// Seal generates a new sealing request for the given input block and pushes
	// the result into the given channel.
	//
	// Note, the method returns immediately and will send the result async. More
	// than one result may also be returned depending on the consensus algorithm.
	Seal(chain ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error

	// SealHash returns the hash of a block prior to it being sealed.
	SealHash(header *types.Header) common.Hash

	// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
	// that a new block should have.
	CalcDifficulty(chain ChainHeaderReader, time uint64, parent *types.Header) *big.Int

	// APIs returns the RPC APIs this consensus engine provides.
	APIs(chain ChainHeaderReader) []rpc.API

	// Close terminates any background threads maintained by the consensus engine.
	Close() error
}

//POW是基于工作证明的共识引擎。
type PoW interface {
	Engine

	//Hashrate返回POW共识发动机的当前采矿哈希拉特。
	Hashrate() float64
}
