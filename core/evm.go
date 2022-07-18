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



// TODO: 封装在外面的对象

package core

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// ChainContext支持从该标题和共识参数从
//当前的区块链在交易处理过程中要使用。
type ChainContext interface {
	// Engine retrieves the chain's consensus engine.
	Engine() consensus.Engine

	// GetHeader returns the header corresponding to the hash/number argument pair.
	GetHeader(common.Hash, uint64) *types.Header
}

// NEwevmblockContext创建了一个新的上下文，用于在EVM中使用。
func NewEVMBlockContext(header *types.Header, chain ChainContext, author *common.Address) vm.BlockContext {
	var (
		beneficiary common.Address
		baseFee     *big.Int
		random      *common.Hash
	)

	// If we don't have an explicit author (i.e. not mining), extract from the header
	if author == nil {
		beneficiary, _ = chain.Engine().Author(header) // Ignore error, we're past header validation
	} else {
		beneficiary = *author
	}
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.Difficulty.Cmp(common.Big0) == 0 {
		random = &header.MixDigest
	}
	return vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     GetHashFn(header, chain),
		Coinbase:    beneficiary,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        new(big.Int).SetUint64(header.Time),
		Difficulty:  new(big.Int).Set(header.Difficulty),
		BaseFee:     baseFee,
		GasLimit:    header.GasLimit,
		Random:      random,
	}
}

// NewEVMTxContext creates a new transaction context for a single transaction.
func NewEVMTxContext(msg Message) vm.TxContext {
	return vm.TxContext{
		Origin:   msg.From(),
		GasPrice: new(big.Int).Set(msg.GasPrice()),
	}
}

// gethashfn返回了gethashfunc，该gethashfunc返回按数字检索标题哈希
func GetHashFn(ref *types.Header, chain ChainContext) func(n uint64) common.Hash {
	// Cache will initially contain [refHash.parent],
	// Then fill up with [refHash.p, refHash.pp, refHash.ppp, ...]
	var cache []common.Hash

	return func(n uint64) common.Hash {
		if ref.Number.Uint64() <= n {
			// This situation can happen if we're doing tracing and using
			// block overrides.
			return common.Hash{}
		}
		// If there's no hash cache yet, make one
		if len(cache) == 0 {
			cache = append(cache, ref.ParentHash)
		}
		if idx := ref.Number.Uint64() - n - 1; idx < uint64(len(cache)) {
			return cache[idx]
		}
		// No luck in the cache, but we can start iterating from the last element we already know
		lastKnownHash := cache[len(cache)-1]
		lastKnownNumber := ref.Number.Uint64() - uint64(len(cache))

		for {
			header := chain.GetHeader(lastKnownHash, lastKnownNumber)
			if header == nil {
				break
			}
			cache = append(cache, header.ParentHash)
			lastKnownHash = header.ParentHash
			lastKnownNumber = header.Number.Uint64() - 1
			if n == lastKnownNumber {
				return lastKnownHash
			}
		}
		return common.Hash{}
	}
}

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
