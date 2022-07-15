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

package gasprice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	errInvalidPercentile = errors.New("invalid reward percentile")
	errRequestBeyondHead = errors.New("request beyond head block")
)

const (
	// maxBlockFetchers is the max number of goroutines to spin up to pull blocks
	// for the fee history calculation (mostly relevant for LES).
	maxBlockFetchers = 4
)

// blockFees represents a single block for processing
type blockFees struct {
	// set by the caller
	blockNumber uint64
	header      *types.Header
	block       *types.Block // only set if reward percentiles are requested
	receipts    types.Receipts
	// filled by processBlock
	results processedFees
	err     error
}

// processedfees包含一个加工块的结果，也用于缓存
type processedFees struct {
	reward               []*big.Int
	baseFee, nextBaseFee *big.Int
	gasUsedRatio         float64
}

// txGasAndReward is sorted in ascending order based on reward
type (
	txGasAndReward struct {
		gasUsed uint64
		reward  *big.Int
	}
	sortGasAndReward []txGasAndReward
)

func (s sortGasAndReward) Len() int { return len(s) }
func (s sortGasAndReward) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s sortGasAndReward) Less(i, j int) bool {
	return s[i].reward.Cmp(s[j].reward) < 0
}

// ProcessBlock使用Blocknumber，标头和可选
//填充的块字段，如果不存在，则从后端检索块，并且
//填写其余的字段。
func (oracle *Oracle) processBlock(bf *blockFees, percentiles []float64) {
	chainconfig := oracle.backend.ChainConfig()
	if bf.results.baseFee = bf.header.BaseFee; bf.results.baseFee == nil {
		bf.results.baseFee = new(big.Int)
	}
	if chainconfig.IsLondon(big.NewInt(int64(bf.blockNumber + 1))) {
		bf.results.nextBaseFee = misc.CalcBaseFee(chainconfig, bf.header)
	} else {
		bf.results.nextBaseFee = new(big.Int)
	}
	bf.results.gasUsedRatio = float64(bf.header.GasUsed) / float64(bf.header.GasLimit)
	if len(percentiles) == 0 {
		// rewards were not requested, return null
		return
	}
	if bf.block == nil || (bf.receipts == nil && len(bf.block.Transactions()) != 0) {
		log.Error("Block or receipts are missing while reward percentiles are requested")
		return
	}

	bf.results.reward = make([]*big.Int, len(percentiles))
	if len(bf.block.Transactions()) == 0 {
		// return an all zero row if there are no transactions to gather data from
		for i := range bf.results.reward {
			bf.results.reward[i] = new(big.Int)
		}
		return
	}

	sorter := make(sortGasAndReward, len(bf.block.Transactions()))
	for i, tx := range bf.block.Transactions() {
		reward, _ := tx.EffectiveGasTip(bf.block.BaseFee())
		sorter[i] = txGasAndReward{gasUsed: bf.receipts[i].GasUsed, reward: reward}
	}
	sort.Stable(sorter)

	var txIndex int
	sumGasUsed := sorter[0].gasUsed

	for i, p := range percentiles {
		thresholdGasUsed := uint64(float64(bf.block.GasUsed()) * p / 100)
		for sumGasUsed < thresholdGasUsed && txIndex < len(bf.block.Transactions())-1 {
			txIndex++
			sumGasUsed += sorter[txIndex].gasUsed
		}
		bf.results.reward[i] = sorter[txIndex].reward
	}
}

// resolveBlockRange resolves the specified block range to absolute block numbers while also
// enforcing backend specific limitations. The pending block and corresponding receipts are
// also returned if requested and available.
// Note: an error is only returned if retrieving the head header has failed. If there are no
// retrievable blocks in the specified range then zero block count is returned with no error.
func (oracle *Oracle) resolveBlockRange(ctx context.Context, lastBlock rpc.BlockNumber, blocks int) (*types.Block, []*types.Receipt, uint64, int, error) {
	var (
		headBlock       rpc.BlockNumber
		pendingBlock    *types.Block
		pendingReceipts types.Receipts
	)
	// query either pending block or head header and set headBlock
	if lastBlock == rpc.PendingBlockNumber {
		if pendingBlock, pendingReceipts = oracle.backend.PendingBlockAndReceipts(); pendingBlock != nil {
			lastBlock = rpc.BlockNumber(pendingBlock.NumberU64())
			headBlock = lastBlock - 1
		} else {
			// pending block not supported by backend, process until latest block
			lastBlock = rpc.LatestBlockNumber
			blocks--
			if blocks == 0 {
				return nil, nil, 0, 0, nil
			}
		}
	}
	if pendingBlock == nil {
		// if pending block is not fetched then we retrieve the head header to get the head block number
		if latestHeader, err := oracle.backend.HeaderByNumber(ctx, rpc.LatestBlockNumber); err == nil {
			headBlock = rpc.BlockNumber(latestHeader.Number.Uint64())
		} else {
			return nil, nil, 0, 0, err
		}
	}
	if lastBlock == rpc.LatestBlockNumber {
		lastBlock = headBlock
	} else if pendingBlock == nil && lastBlock > headBlock {
		return nil, nil, 0, 0, fmt.Errorf("%w: requested %d, head %d", errRequestBeyondHead, lastBlock, headBlock)
	}
	// ensure not trying to retrieve before genesis
	if rpc.BlockNumber(blocks) > lastBlock+1 {
		blocks = int(lastBlock + 1)
	}
	return pendingBlock, pendingReceipts, uint64(lastBlock), blocks, nil
}

// FeeHistory returns data relevant for fee estimation based on the specified range of blocks.
// The range can be specified either with absolute block numbers or ending with the latest
// or pending block. Backends may or may not support gathering data from the pending block
// or blocks older than a certain age (specified in maxHistory). The first block of the
// actually processed range is returned to avoid ambiguity when parts of the requested range
// are not available or when the head has changed during processing this request.
// Three arrays are returned based on the processed blocks:
// - reward: the requested percentiles of effective priority fees per gas of transactions in each
//   block, sorted in ascending order and weighted by gas used.
// - baseFee: base fee per gas in the given block
// - gasUsedRatio: gasUsed/gasLimit in the given block
// Note: baseFee includes the next block after the newest of the returned range, because this
// value can be derived from the newest block.
func (oracle *Oracle) FeeHistory(ctx context.Context, blocks int, unresolvedLastBlock rpc.BlockNumber, rewardPercentiles []float64) (*big.Int, [][]*big.Int, []*big.Int, []float64, error) {
	if blocks < 1 {
		return common.Big0, nil, nil, nil, nil // returning with no data and no error means there are no retrievable blocks
	}
	maxFeeHistory := oracle.maxHeaderHistory
	if len(rewardPercentiles) != 0 {
		maxFeeHistory = oracle.maxBlockHistory
	}
	if blocks > maxFeeHistory {
		log.Warn("Sanitizing fee history length", "requested", blocks, "truncated", maxFeeHistory)
		blocks = maxFeeHistory
	}
	for i, p := range rewardPercentiles {
		if p < 0 || p > 100 {
			return common.Big0, nil, nil, nil, fmt.Errorf("%w: %f", errInvalidPercentile, p)
		}
		if i > 0 && p < rewardPercentiles[i-1] {
			return common.Big0, nil, nil, nil, fmt.Errorf("%w: #%d:%f > #%d:%f", errInvalidPercentile, i-1, rewardPercentiles[i-1], i, p)
		}
	}
	var (
		pendingBlock    *types.Block
		pendingReceipts []*types.Receipt
		err             error
	)
	pendingBlock, pendingReceipts, lastBlock, blocks, err := oracle.resolveBlockRange(ctx, unresolvedLastBlock, blocks)
	if err != nil || blocks == 0 {
		return common.Big0, nil, nil, nil, err
	}
	oldestBlock := lastBlock + 1 - uint64(blocks)

	var (
		next    = oldestBlock
		results = make(chan *blockFees, blocks)
	)
	percentileKey := make([]byte, 8*len(rewardPercentiles))
	for i, p := range rewardPercentiles {
		binary.LittleEndian.PutUint64(percentileKey[i*8:(i+1)*8], math.Float64bits(p))
	}
	for i := 0; i < maxBlockFetchers && i < blocks; i++ {
		go func() {
			for {
				// Retrieve the next block number to fetch with this goroutine
				blockNumber := atomic.AddUint64(&next, 1) - 1
				if blockNumber > lastBlock {
					return
				}

				fees := &blockFees{blockNumber: blockNumber}
				if pendingBlock != nil && blockNumber >= pendingBlock.NumberU64() {
					fees.block, fees.receipts = pendingBlock, pendingReceipts
					fees.header = fees.block.Header()
					oracle.processBlock(fees, rewardPercentiles)
					results <- fees
				} else {
					cacheKey := struct {
						number      uint64
						percentiles string
					}{blockNumber, string(percentileKey)}

					if p, ok := oracle.historyCache.Get(cacheKey); ok {
						fees.results = p.(processedFees)
						results <- fees
					} else {
						if len(rewardPercentiles) != 0 {
							fees.block, fees.err = oracle.backend.BlockByNumber(ctx, rpc.BlockNumber(blockNumber))
							if fees.block != nil && fees.err == nil {
								fees.receipts, fees.err = oracle.backend.GetReceipts(ctx, fees.block.Hash())
								fees.header = fees.block.Header()
							}
						} else {
							fees.header, fees.err = oracle.backend.HeaderByNumber(ctx, rpc.BlockNumber(blockNumber))
						}
						if fees.header != nil && fees.err == nil {
							oracle.processBlock(fees, rewardPercentiles)
							if fees.err == nil {
								oracle.historyCache.Add(cacheKey, fees.results)
							}
						}
						// send to results even if empty to guarantee that blocks items are sent in total
						results <- fees
					}
				}
			}
		}()
	}
	var (
		reward       = make([][]*big.Int, blocks)
		baseFee      = make([]*big.Int, blocks+1)
		gasUsedRatio = make([]float64, blocks)
		firstMissing = blocks
	)
	for ; blocks > 0; blocks-- {
		fees := <-results
		if fees.err != nil {
			return common.Big0, nil, nil, nil, fees.err
		}
		i := int(fees.blockNumber - oldestBlock)
		if fees.results.baseFee != nil {
			reward[i], baseFee[i], baseFee[i+1], gasUsedRatio[i] = fees.results.reward, fees.results.baseFee, fees.results.nextBaseFee, fees.results.gasUsedRatio
		} else {
			// getting no block and no error means we are requesting into the future (might happen because of a reorg)
			if i < firstMissing {
				firstMissing = i
			}
		}
	}
	if firstMissing == 0 {
		return common.Big0, nil, nil, nil, nil
	}
	if len(rewardPercentiles) != 0 {
		reward = reward[:firstMissing]
	} else {
		reward = nil
	}
	baseFee, gasUsedRatio = baseFee[:firstMissing+1], gasUsedRatio[:firstMissing]
	return new(big.Int).SetUint64(oldestBlock), reward, baseFee, gasUsedRatio, nil
}
