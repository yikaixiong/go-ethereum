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

package miner

import (
	"container/ring"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

//未经证实的块集使用了chainretriever，以验证是否以前是否
//开采块是不管是否是规范链的一部分。
type chainRetriever interface {
	// GetHeaderByNumber retrieves the canonical header associated with a block number.
	GetHeaderByNumber(number uint64) *types.Header

	// GetBlockByNumber retrieves the canonical block associated with a block number.
	GetBlockByNumber(number uint64) *types.Block
}

// unconfirmedBlock is a small collection of metadata about a locally mined block
// that is placed into a unconfirmed set for canonical chain inclusion tracking.
type unconfirmedBlock struct {
	index uint64
	hash  common.Hash
}

// unconfirmedBlocks implements a data structure to maintain locally mined blocks
// have not yet reached enough maturity to guarantee chain inclusion. It is
// used by the miner to provide logs to the user when a previously mined block
// has a high enough guarantee to not be reorged out of the canonical chain.
type unconfirmedBlocks struct {
	chain  chainRetriever // Blockchain to verify canonical status through
	depth  uint           // Depth after which to discard previous blocks
	blocks *ring.Ring     // Block infos to allow canonical chain cross checks
	lock   sync.Mutex     // Protects the fields from concurrent access
}

// newUnconfirmedBlocks returns new data structure to track currently unconfirmed blocks.
func newUnconfirmedBlocks(chain chainRetriever, depth uint) *unconfirmedBlocks {
	return &unconfirmedBlocks{
		chain: chain,
		depth: depth,
	}
}

// Insert adds a new block to the set of unconfirmed ones.
func (set *unconfirmedBlocks) Insert(index uint64, hash common.Hash) {
	// If a new block was mined locally, shift out any old enough blocks
	set.Shift(index)

	// Create the new item as its own ring
	item := ring.New(1)
	item.Value = &unconfirmedBlock{
		index: index,
		hash:  hash,
	}
	// Set as the initial ring or append to the end
	set.lock.Lock()
	defer set.lock.Unlock()

	if set.blocks == nil {
		set.blocks = item
	} else {
		set.blocks.Move(-1).Link(item)
	}
	// Display a log for the user to notify of a new mined block unconfirmed
	log.Info("🔨 mined potential block", "number", index, "hash", hash)
}

// Shift drops all unconfirmed blocks from the set which exceed the unconfirmed sets depth
// allowance, checking them against the canonical chain for inclusion or staleness
// report.
func (set *unconfirmedBlocks) Shift(height uint64) {
	set.lock.Lock()
	defer set.lock.Unlock()

	for set.blocks != nil {
		// Retrieve the next unconfirmed block and abort if too fresh
		next := set.blocks.Value.(*unconfirmedBlock)
		if next.index+uint64(set.depth) > height {
			break
		}
		// Block seems to exceed depth allowance, check for canonical status
		header := set.chain.GetHeaderByNumber(next.index)
		switch {
		case header == nil:
			log.Warn("Failed to retrieve header of mined block", "number", next.index, "hash", next.hash)
		case header.Hash() == next.hash:
			log.Info("🔗 block reached canonical chain", "number", next.index, "hash", next.hash)
		default:
			// Block is not canonical, check whether we have an uncle or a lost block
			included := false
			for number := next.index; !included && number < next.index+uint64(set.depth) && number <= height; number++ {
				if block := set.chain.GetBlockByNumber(number); block != nil {
					for _, uncle := range block.Uncles() {
						if uncle.Hash() == next.hash {
							included = true
							break
						}
					}
				}
			}
			if included {
				log.Info("⑂ block became an uncle", "number", next.index, "hash", next.hash)
			} else {
				log.Info("😱 block lost", "number", next.index, "hash", next.hash)
			}
		}
		// Drop the block out of the ring
		if set.blocks.Value == set.blocks.Next().Value {
			set.blocks = nil
		} else {
			set.blocks = set.blocks.Move(-1)
			set.blocks.Unlink(1)
			set.blocks = set.blocks.Move(1)
		}
	}
}
