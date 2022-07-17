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


// TODO:   信标链

package beacon

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

// 验证协议常数。
var (
	beaconDifficulty = common.Big0          // The default block difficulty in the beacon consensus
	beaconNonce      = types.EncodeNonce(0) // The default block nonce in the beacon consensus
)

// 标记块的各种错误消息无效。这些应该是私人的
//防止发动机在其余部分中引用发动机的错误
//代码库，如果引擎被换掉，则可以固有地打破。请公共
//错误类型输入共识软件包。
var (
	errTooManyUncles    = errors.New("too many uncles")
	errInvalidNonce     = errors.New("invalid nonce")
	errInvalidUncleHash = errors.New("invalid uncle hash")
	errInvalidTimestamp = errors.New("invalid timestamp")
)

//信标是结合ETH1共识和有验证证明的共识引擎
// 算法。里面有一个特殊的标志可以决定是否使用遗产共识
//规则或新规则。过渡规则在ETH1/2合并规范中描述。
// https://github.com/ethereum/eips/blob/master/eips/eips/eip-3675.md
//
//这里的灯塔是一个半功能共识引擎，具有部分功能
//仅用于必要的共识检查。传统共识引擎可以是任何
//引擎实现了共识接口（Beaccon本身除外）。
type Beacon struct {
	ethone consensus.Engine // Original consensus engine used in eth1, e.g. ethash or clique
}

// New用给定的ETH1引擎创建了共识引擎。
func New(ethone consensus.Engine) *Beacon {
	if _, ok := ethone.(*Beacon); ok {
		panic("nested consensus engine")
	}
	return &Beacon{ethone: ethone}
}

// 作者实现了共识。发动机，返回了该块的验证作者。
func (beacon *Beacon) Author(header *types.Header) (common.Address, error) {
	if !beacon.IsPoSHeader(header) {
		return beacon.ethone.Author(header)
	}
	return header.Coinbase, nil
}

// verifyheader检查标头是否符合
//库存以太坊共识引擎。
func (beacon *Beacon) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	reached, _ := IsTTDReached(chain, header.ParentHash, header.Number.Uint64()-1)
	if !reached {
		return beacon.ethone.VerifyHeader(chain, header, seal)
	}
	// 短路如果父母未知
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// 通过理智检查，进行适当的验证
	return beacon.verifyHeader(chain, header, parent)
}

// verifyheaders类似于verifyheader，但验证了一批标题
//同时。该方法返回退出渠道以中止操作，
//结果通道检索异步验证。
//验证者期望将标题订购和连续。
func (beacon *Beacon) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	if !beacon.IsPoSHeader(headers[len(headers)-1]) {
		return beacon.ethone.VerifyHeaders(chain, headers, seals)
	}
	var (
		preHeaders  []*types.Header
		postHeaders []*types.Header
		preSeals    []bool
	)
	for index, header := range headers {
		if beacon.IsPoSHeader(header) {
			preHeaders = headers[:index]
			postHeaders = headers[index:]
			preSeals = seals[:index]
			break
		}
	}

	// All the headers have passed the transition point, use new rules.
	if len(preHeaders) == 0 {
		return beacon.verifyHeaders(chain, headers, nil)
	}

	// The transition point exists in the middle, separate the headers
	// into two batches and apply different verification rules for them.
	var (
		abort   = make(chan struct{})
		results = make(chan error, len(headers))
	)
	go func() {
		var (
			old, new, out      = 0, len(preHeaders), 0
			errors             = make([]error, len(headers))
			done               = make([]bool, len(headers))
			oldDone, oldResult = beacon.ethone.VerifyHeaders(chain, preHeaders, preSeals)
			newDone, newResult = beacon.verifyHeaders(chain, postHeaders, preHeaders[len(preHeaders)-1])
		)
		// Verify that pre-merge headers don't overflow the TTD
		if index, err := verifyTerminalPoWBlock(chain, preHeaders); err != nil {
			// Mark all subsequent pow headers with the error.
			for i := index; i < len(preHeaders); i++ {
				errors[i], done[i] = err, true
			}
		}
		// Collect the results
		for {
			for ; done[out]; out++ {
				results <- errors[out]
				if out == len(headers)-1 {
					return
				}
			}
			select {
			case err := <-oldResult:
				if !done[old] { // skip TTD-verified failures
					errors[old], done[old] = err, true
				}
				old++
			case err := <-newResult:
				errors[new], done[new] = err, true
				new++
			case <-abort:
				close(oldDone)
				close(newDone)
				return
			}
		}
	}()
	return abort, results
}

// verifyterminalpowblock验证预先确认为规格的确认
// WRT。他们的完全困难。
//期望：
//-预开头至少为1个元素
//-要正确存储在链条中的标头元素的父
//-预先设置难度的预开头
//-最后一个是终端块的元素
func verifyTerminalPoWBlock(chain consensus.ChainHeaderReader, preHeaders []*types.Header) (int, error) {
	td := chain.GetTd(preHeaders[0].ParentHash, preHeaders[0].Number.Uint64()-1)
	if td == nil {
		return 0, consensus.ErrUnknownAncestor
	}
	td = new(big.Int).Set(td)
	// Check that all blocks before the last one are below the TTD
	for i, head := range preHeaders {
		if td.Cmp(chain.Config().TerminalTotalDifficulty) >= 0 {
			return i, consensus.ErrInvalidTerminalBlock
		}
		td.Add(td, head.Difficulty)
	}
	// Check that the last block is the terminal block
	if td.Cmp(chain.Config().TerminalTotalDifficulty) < 0 {
		return len(preHeaders) - 1, consensus.ErrInvalidTerminalBlock
	}
	return 0, nil
}

//验证叔叔验证给定块的叔叔是否符合共识
//以太坊共识引擎的规则。
func (beacon *Beacon) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if !beacon.IsPoSHeader(block.Header()) {
		return beacon.ethone.VerifyUncles(chain, block)
	}
	// Verify that there is no uncle block. It's explicitly disabled in the beacon
	if len(block.Uncles()) > 0 {
		return errTooManyUncles
	}
	return nil
}

// verifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum consensus engine. The difference between the beacon and classic is
// (a) The following fields are expected to be constants:
//     - difficulty is expected to be 0
// 	   - nonce is expected to be 0
//     - unclehash is expected to be Hash(emptyHeader)
//     to be the desired constants
// (b) we don't verify if a block is in the future anymore
// (c) the extradata is limited to 32 bytes
func (beacon *Beacon) verifyHeader(chain consensus.ChainHeaderReader, header, parent *types.Header) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if len(header.Extra) > 32 {
		return fmt.Errorf("extra-data longer than 32 bytes (%d)", len(header.Extra))
	}
	// Verify the seal parts. Ensure the nonce and uncle hash are the expected value.
	if header.Nonce != beaconNonce {
		return errInvalidNonce
	}
	if header.UncleHash != types.EmptyUncleHash {
		return errInvalidUncleHash
	}
	// Verify the timestamp
	if header.Time <= parent.Time {
		return errInvalidTimestamp
	}
	// Verify the block's difficulty to ensure it's the default constant
	if beaconDifficulty.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, beaconDifficulty)
	}
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(common.Big1) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the header's EIP-1559 attributes.
	return misc.VerifyEip1559Header(chain.Config(), parent, header)
}

// verifyHeaders is similar to verifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications. An additional parent
// header will be passed if the relevant header is not in the database yet.
func (beacon *Beacon) verifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, ancestor *types.Header) (chan<- struct{}, <-chan error) {
	var (
		abort   = make(chan struct{})
		results = make(chan error, len(headers))
	)
	go func() {
		for i, header := range headers {
			var parent *types.Header
			if i == 0 {
				if ancestor != nil {
					parent = ancestor
				} else {
					parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
				}
			} else if headers[i-1].Hash() == headers[i].ParentHash {
				parent = headers[i-1]
			}
			if parent == nil {
				select {
				case <-abort:
					return
				case results <- consensus.ErrUnknownAncestor:
				}
				continue
			}
			err := beacon.verifyHeader(chain, header, parent)
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// Prepare implements consensus.Engine, initializing the difficulty field of a
// header to conform to the beacon protocol. The changes are done inline.
func (beacon *Beacon) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Transition isn't triggered yet, use the legacy rules for preparation.
	reached, err := IsTTDReached(chain, header.ParentHash, header.Number.Uint64()-1)
	if err != nil {
		return err
	}
	if !reached {
		return beacon.ethone.Prepare(chain, header)
	}
	header.Difficulty = beaconDifficulty
	return nil
}

// Finalize implements consensus.Engine, setting the final state on the header
func (beacon *Beacon) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
	// Finalize is different with Prepare, it can be used in both block generation
	// and verification. So determine the consensus rules by header type.
	if !beacon.IsPoSHeader(header) {
		beacon.ethone.Finalize(chain, header, state, txs, uncles)
		return
	}
	// The block reward is no longer handled here. It's done by the
	// external consensus engine.
	header.Root = state.IntermediateRoot(true)
}

// FinalizeAndAssemble implements consensus.Engine, setting the final state and
// assembling the block.
func (beacon *Beacon) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// FinalizeAndAssemble is different with Prepare, it can be used in both block
	// generation and verification. So determine the consensus rules by header type.
	if !beacon.IsPoSHeader(header) {
		return beacon.ethone.FinalizeAndAssemble(chain, header, state, txs, uncles, receipts)
	}
	// Finalize and assemble the block
	beacon.Finalize(chain, header, state, txs, uncles)
	return types.NewBlock(header, txs, uncles, receipts, trie.NewStackTrie(nil)), nil
}

// Seal generates a new sealing request for the given input block and pushes
// the result into the given channel.
//
// Note, the method returns immediately and will send the result async. More
// than one result may also be returned depending on the consensus algorithm.
func (beacon *Beacon) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	if !beacon.IsPoSHeader(block.Header()) {
		return beacon.ethone.Seal(chain, block, results, stop)
	}
	// The seal verification is done by the external consensus engine,
	// return directly without pushing any block back. In another word
	// beacon won't return any result by `results` channel which may
	// blocks the receiver logic forever.
	return nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (beacon *Beacon) SealHash(header *types.Header) common.Hash {
	return beacon.ethone.SealHash(header)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func (beacon *Beacon) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	// Transition isn't triggered yet, use the legacy rules for calculation
	if reached, _ := IsTTDReached(chain, parent.Hash(), parent.Number.Uint64()); !reached {
		return beacon.ethone.CalcDifficulty(chain, time, parent)
	}
	return beaconDifficulty
}

// APIs implements consensus.Engine, returning the user facing RPC APIs.
func (beacon *Beacon) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return beacon.ethone.APIs(chain)
}

// Close shutdowns the consensus engine
func (beacon *Beacon) Close() error {
	return beacon.ethone.Close()
}

// IsPoSHeader reports the header belongs to the PoS-stage with some special fields.
// This function is not suitable for a part of APIs like Prepare or CalcDifficulty
// because the header difficulty is not set yet.
func (beacon *Beacon) IsPoSHeader(header *types.Header) bool {
	if header.Difficulty == nil {
		panic("IsPoSHeader called with invalid difficulty")
	}
	return header.Difficulty.Cmp(beaconDifficulty) == 0
}

// InnerEngine returns the embedded eth1 consensus engine.
func (beacon *Beacon) InnerEngine() consensus.Engine {
	return beacon.ethone
}

// SetThreads updates the mining threads. Delegate the call
// to the eth1 engine if it's threaded.
func (beacon *Beacon) SetThreads(threads int) {
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := beacon.ethone.(threaded); ok {
		th.SetThreads(threads)
	}
}

// IsTTDReached checks if the TotalTerminalDifficulty has been surpassed on the `parentHash` block.
// It depends on the parentHash already being stored in the database.
// If the parentHash is not stored in the database a UnknownAncestor error is returned.
func IsTTDReached(chain consensus.ChainHeaderReader, parentHash common.Hash, number uint64) (bool, error) {
	if chain.Config().TerminalTotalDifficulty == nil {
		return false, nil
	}
	td := chain.GetTd(parentHash, number)
	if td == nil {
		return false, consensus.ErrUnknownAncestor
	}
	return td.Cmp(chain.Config().TerminalTotalDifficulty) >= 0, nil
}
