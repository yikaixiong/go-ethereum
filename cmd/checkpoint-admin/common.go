//版权所有2019年作者
//此文件是Go-Ethereum的一部分。
//
// Go-Ethereum是免费软件：您可以重新分配它和/或修改
//根据GNU通用公共许可证的条款发布
//免费软件基金会（许可证的3版本）或
//（根据您的选择）任何以后的版本。
//
// go-ethereum的分发是希望它有用的
//但没有任何保修；甚至没有暗示的保证
//适合或适合特定目的的健身。看到
// GNU通用公共许可证以获取更多详细信息。
//
//您应该收到GNU通用公共许可证的副本
//与Go-Ethereum一起。如果不是，请参见<http://www.gnu.org/licenses/>。

package main

import (
	"strconv"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/external"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts/checkpointoracle"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfave/cli/v2"
)

// 使用指定的远程URL创建客户端。
func newClient(ctx *cli.Context) *ethclient.Client {
	client, err := ethclient.Dial(ctx.String(nodeURLFlag.Name))
	if err != nil {
		utils.Fatalf("Failed to connect to Ethereum node: %v", err)
	}
	return client
}

// NEWRPCCLIENT使用指定的节点URL创建RPC客户端。
func newRPCClient(url string) *rpc.Client {
	client, err := rpc.Dial(url)
	if err != nil {
		utils.Fatalf("Failed to connect to Ethereum node: %v", err)
	}
	return client
}

//GetContractAddr通过
// RPC请求。
func getContractAddr(client *rpc.Client) common.Address {
	var addr string
	if err := client.Call(&addr, "les_getCheckpointContractAddress"); err != nil {
		utils.Fatalf("Failed to fetch checkpoint oracle address: %v", err)
	}
	return common.HexToAddress(addr)
}

// GetCheckpoint检索指定的检查点或最新的检查点
//通过RPC请求。
func getCheckpoint(ctx *cli.Context, client *rpc.Client) *params.TrustedCheckpoint {
	var checkpoint *params.TrustedCheckpoint

	if ctx.IsSet(indexFlag.Name) {
		var result [3]string
		index := uint64(ctx.Int64(indexFlag.Name))
		if err := client.Call(&result, "les_getCheckpoint", index); err != nil {
			utils.Fatalf("Failed to get local checkpoint %v, please ensure the les API is exposed", err)
		}
		checkpoint = &params.TrustedCheckpoint{
			SectionIndex: index,
			SectionHead:  common.HexToHash(result[0]),
			CHTRoot:      common.HexToHash(result[1]),
			BloomRoot:    common.HexToHash(result[2]),
		}
	} else {
		var result [4]string
		err := client.Call(&result, "les_latestCheckpoint")
		if err != nil {
			utils.Fatalf("Failed to get local checkpoint %v, please ensure the les API is exposed", err)
		}
		index, err := strconv.ParseUint(result[0], 0, 64)
		if err != nil {
			utils.Fatalf("Failed to parse checkpoint index %v", err)
		}
		checkpoint = &params.TrustedCheckpoint{
			SectionIndex: index,
			SectionHead:  common.HexToHash(result[1]),
			CHTRoot:      common.HexToHash(result[2]),
			BloomRoot:    common.HexToHash(result[3]),
		}
	}
	return checkpoint
}

//Newtract与指定的注册商合同实例创建
// Mainnet或TestNet的合同地址或默认合同。
func newContract(client *rpc.Client) (common.Address, *checkpointoracle.CheckpointOracle) {
	addr := getContractAddr(client)
	if addr == (common.Address{}) {
		utils.Fatalf("No specified registrar contract address")
	}
	contract, err := checkpointoracle.NewCheckpointOracle(addr, ethclient.NewClient(client))
	if err != nil {
		utils.Fatalf("Failed to setup registrar contract %s: %v", addr, err)
	}
	return addr, contract
}

// newClefSigner sets up a clef backend and returns a clef transaction signer.
func newClefSigner(ctx *cli.Context) *bind.TransactOpts {
	clef, err := external.NewExternalSigner(ctx.String(clefURLFlag.Name))
	if err != nil {
		utils.Fatalf("Failed to create clef signer %v", err)
	}
	return bind.NewClefTransactor(clef, accounts.Account{Address: common.HexToAddress(ctx.String(signerFlag.Name))})
}
