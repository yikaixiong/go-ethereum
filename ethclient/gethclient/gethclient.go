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

//软件包Gethclient为Geth特定的API提供了RPC客户端。
package gethclient

import (
	"context"
	"math/big"
	"runtime"
	"runtime/debug"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

//客户端是围绕RPC.Client的包装器，可实现Geth特定功能。
//
//如果要使用标准化的以太坊RPC功能，请改用EthClient.Client。
type Client struct {
	c *rpc.Client
}

// 新创建使用给定RPC客户端的客户端。
func New(c *rpc.Client) *Client {
	return &Client{c}
}

// CreateAccessList试图根据基于特定事务的特定交易创建访问列表
//当前待处理的区块链状态。
func (ec *Client) CreateAccessList(ctx context.Context, msg ethereum.CallMsg) (*types.AccessList, uint64, string, error) {
	type accessListResult struct {
		Accesslist *types.AccessList `json:"accessList"`
		Error      string            `json:"error,omitempty"`
		GasUsed    hexutil.Uint64    `json:"gasUsed"`
	}
	var result accessListResult
	if err := ec.c.CallContext(ctx, &result, "eth_createAccessList", toCallArg(msg)); err != nil {
		return nil, 0, "", err
	}
	return result.Accesslist, uint64(result.GasUsed), result.Error, nil
}

// Accountresult是防止操作的结果。
type AccountResult struct {
	Address      common.Address  `json:"address"`
	AccountProof []string        `json:"accountProof"`
	Balance      *big.Int        `json:"balance"`
	CodeHash     common.Hash     `json:"codeHash"`
	Nonce        uint64          `json:"nonce"`
	StorageHash  common.Hash     `json:"storageHash"`
	StorageProof []StorageResult `json:"storageProof"`
}

// StorageResult provides a proof for a key-value pair.
type StorageResult struct {
	Key   string   `json:"key"`
	Value *big.Int `json:"value"`
	Proof []string `json:"proof"`
}

// GetProof returns the account and storage values of the specified account including the Merkle-proof.
// The block number can be nil, in which case the value is taken from the latest known block.
func (ec *Client) GetProof(ctx context.Context, account common.Address, keys []string, blockNumber *big.Int) (*AccountResult, error) {

	type storageResult struct {
		Key   string       `json:"key"`
		Value *hexutil.Big `json:"value"`
		Proof []string     `json:"proof"`
	}

	type accountResult struct {
		Address      common.Address  `json:"address"`
		AccountProof []string        `json:"accountProof"`
		Balance      *hexutil.Big    `json:"balance"`
		CodeHash     common.Hash     `json:"codeHash"`
		Nonce        hexutil.Uint64  `json:"nonce"`
		StorageHash  common.Hash     `json:"storageHash"`
		StorageProof []storageResult `json:"storageProof"`
	}

	var res accountResult
	err := ec.c.CallContext(ctx, &res, "eth_getProof", account, keys, toBlockNumArg(blockNumber))
	// Turn hexutils back to normal datatypes
	storageResults := make([]StorageResult, 0, len(res.StorageProof))
	for _, st := range res.StorageProof {
		storageResults = append(storageResults, StorageResult{
			Key:   st.Key,
			Value: st.Value.ToInt(),
			Proof: st.Proof,
		})
	}
	result := AccountResult{
		Address:      res.Address,
		AccountProof: res.AccountProof,
		Balance:      res.Balance.ToInt(),
		Nonce:        uint64(res.Nonce),
		CodeHash:     res.CodeHash,
		StorageHash:  res.StorageHash,
		StorageProof: storageResults,
	}
	return &result, err
}

// OverrideAccount specifies the state of an account to be overridden.
type OverrideAccount struct {
	Nonce     uint64                      `json:"nonce"`
	Code      []byte                      `json:"code"`
	Balance   *big.Int                    `json:"balance"`
	State     map[common.Hash]common.Hash `json:"state"`
	StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
}

// CallContract executes a message call transaction, which is directly executed in the VM
// of the node, but never mined into the blockchain.
//
// blockNumber selects the block height at which the call runs. It can be nil, in which
// case the code is taken from the latest known block. Note that state from very old
// blocks might not be available.
//
// overrides specifies a map of contract states that should be overwritten before executing
// the message call.
// Please use ethclient.CallContract instead if you don't need the override functionality.
func (ec *Client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int, overrides *map[common.Address]OverrideAccount) ([]byte, error) {
	var hex hexutil.Bytes
	err := ec.c.CallContext(
		ctx, &hex, "eth_call", toCallArg(msg),
		toBlockNumArg(blockNumber), toOverrideMap(overrides),
	)
	return hex, err
}

// GCStats retrieves the current garbage collection stats from a geth node.
func (ec *Client) GCStats(ctx context.Context) (*debug.GCStats, error) {
	var result debug.GCStats
	err := ec.c.CallContext(ctx, &result, "debug_gcStats")
	return &result, err
}

// MemStats retrieves the current memory stats from a geth node.
func (ec *Client) MemStats(ctx context.Context) (*runtime.MemStats, error) {
	var result runtime.MemStats
	err := ec.c.CallContext(ctx, &result, "debug_memStats")
	return &result, err
}

// SetHead sets the current head of the local chain by block number.
// Note, this is a destructive action and may severely damage your chain.
// Use with extreme caution.
func (ec *Client) SetHead(ctx context.Context, number *big.Int) error {
	return ec.c.CallContext(ctx, nil, "debug_setHead", toBlockNumArg(number))
}

// GetNodeInfo retrieves the node info of a geth node.
func (ec *Client) GetNodeInfo(ctx context.Context) (*p2p.NodeInfo, error) {
	var result p2p.NodeInfo
	err := ec.c.CallContext(ctx, &result, "admin_nodeInfo")
	return &result, err
}

// SubscribePendingTransactions subscribes to new pending transactions.
func (ec *Client) SubscribePendingTransactions(ctx context.Context, ch chan<- common.Hash) (*rpc.ClientSubscription, error) {
	return ec.c.EthSubscribe(ctx, ch, "newPendingTransactions")
}

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	pending := big.NewInt(-1)
	if number.Cmp(pending) == 0 {
		return "pending"
	}
	return hexutil.EncodeBig(number)
}

func toCallArg(msg ethereum.CallMsg) interface{} {
	arg := map[string]interface{}{
		"from": msg.From,
		"to":   msg.To,
	}
	if len(msg.Data) > 0 {
		arg["data"] = hexutil.Bytes(msg.Data)
	}
	if msg.Value != nil {
		arg["value"] = (*hexutil.Big)(msg.Value)
	}
	if msg.Gas != 0 {
		arg["gas"] = hexutil.Uint64(msg.Gas)
	}
	if msg.GasPrice != nil {
		arg["gasPrice"] = (*hexutil.Big)(msg.GasPrice)
	}
	return arg
}

func toOverrideMap(overrides *map[common.Address]OverrideAccount) interface{} {
	if overrides == nil {
		return nil
	}
	type overrideAccount struct {
		Nonce     hexutil.Uint64              `json:"nonce"`
		Code      hexutil.Bytes               `json:"code"`
		Balance   *hexutil.Big                `json:"balance"`
		State     map[common.Hash]common.Hash `json:"state"`
		StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
	}
	result := make(map[common.Address]overrideAccount)
	for addr, override := range *overrides {
		result[addr] = overrideAccount{
			Nonce:     hexutil.Uint64(override.Nonce),
			Code:      override.Code,
			Balance:   (*hexutil.Big)(override.Balance),
			State:     override.State,
			StateDiff: override.StateDiff,
		}
	}
	return &result
}
