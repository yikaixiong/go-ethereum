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
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/contracts/checkpointoracle"
	"github.com/ethereum/go-ethereum/contracts/checkpointoracle/contract"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfave/cli/v2"
)

var commandDeploy = &cli.Command{
	Name:  "deploy",
	Usage: "Deploy a new checkpoint oracle contract",
	Flags: []cli.Flag{
		nodeURLFlag,
		clefURLFlag,
		signerFlag,
		signersFlag,
		thresholdFlag,
	},
	Action: deploy,
}

var commandSign = &cli.Command{
	Name:  "sign",
	Usage: "Sign the checkpoint with the specified key",
	Flags: []cli.Flag{
		nodeURLFlag,
		clefURLFlag,
		signerFlag,
		indexFlag,
		hashFlag,
		oracleFlag,
	},
	Action: sign,
}

var commandPublish = &cli.Command{
	Name:  "publish",
	Usage: "Publish a checkpoint into the oracle",
	Flags: []cli.Flag{
		nodeURLFlag,
		clefURLFlag,
		signerFlag,
		indexFlag,
		signaturesFlag,
	},
	Action: publish,
}

//部署部署检查点注册商合同。
//
//注意部署合同的网络取决于
//连接节点所在的网络。
func deploy(ctx *cli.Context) error {
	// Gather all the addresses that should be permitted to sign
	var addrs []common.Address
	for _, account := range strings.Split(ctx.String(signersFlag.Name), ",") {
		if trimmed := strings.TrimSpace(account); !common.IsHexAddress(trimmed) {
			utils.Fatalf("Invalid account in --signers: '%s'", trimmed)
		}
		addrs = append(addrs, common.HexToAddress(account))
	}
	// Retrieve and validate the signing threshold
	needed := ctx.Int(thresholdFlag.Name)
	if needed == 0 || needed > len(addrs) {
		utils.Fatalf("Invalid signature threshold %d", needed)
	}
	// Print a summary to ensure the user understands what they're signing
	fmt.Printf("Deploying new checkpoint oracle:\n\n")
	for i, addr := range addrs {
		fmt.Printf("Admin %d => %s\n", i+1, addr.Hex())
	}
	fmt.Printf("\nSignatures needed to publish: %d\n", needed)

	// setup clef signer, create an abigen transactor and an RPC client
	transactor, client := newClefSigner(ctx), newClient(ctx)

	// Deploy the checkpoint oracle
	fmt.Println("Sending deploy request to Clef...")
	oracle, tx, _, err := contract.DeployCheckpointOracle(transactor, client, addrs, big.NewInt(int64(params.CheckpointFrequency)),
		big.NewInt(int64(params.CheckpointProcessConfirmations)), big.NewInt(int64(needed)))
	if err != nil {
		utils.Fatalf("Failed to deploy checkpoint oracle %v", err)
	}
	log.Info("Deployed checkpoint oracle", "address", oracle, "tx", tx.Hash().Hex())

	return nil
}

//符号为特定检查点创建签名
//使用本地密钥。只有合同管理员才有许可
//签名检查点。
func sign(ctx *cli.Context) error {
	var (
		offline bool // The indicator whether we sign checkpoint by offline.
		chash   common.Hash
		cindex  uint64
		address common.Address

		node   *rpc.Client
		oracle *checkpointoracle.CheckpointOracle
	)
	if !ctx.IsSet(nodeURLFlag.Name) {
		// Offline mode signing
		offline = true
		if !ctx.IsSet(hashFlag.Name) {
			utils.Fatalf("Please specify the checkpoint hash (--hash) to sign in offline mode")
		}
		chash = common.HexToHash(ctx.String(hashFlag.Name))

		if !ctx.IsSet(indexFlag.Name) {
			utils.Fatalf("Please specify checkpoint index (--index) to sign in offline mode")
		}
		cindex = ctx.Uint64(indexFlag.Name)

		if !ctx.IsSet(oracleFlag.Name) {
			utils.Fatalf("Please specify oracle address (--oracle) to sign in offline mode")
		}
		address = common.HexToAddress(ctx.String(oracleFlag.Name))
	} else {
		// Interactive mode signing, retrieve the data from the remote node
		node = newRPCClient(ctx.String(nodeURLFlag.Name))

		checkpoint := getCheckpoint(ctx, node)
		chash, cindex, address = checkpoint.Hash(), checkpoint.SectionIndex, getContractAddr(node)

		// Check the validity of checkpoint
		reqCtx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelFn()

		head, err := ethclient.NewClient(node).HeaderByNumber(reqCtx, nil)
		if err != nil {
			return err
		}
		num := head.Number.Uint64()
		if num < ((cindex+1)*params.CheckpointFrequency + params.CheckpointProcessConfirmations) {
			utils.Fatalf("Invalid future checkpoint")
		}
		_, oracle = newContract(node)
		latest, _, h, err := oracle.Contract().GetLatestCheckpoint(nil)
		if err != nil {
			return err
		}
		if cindex < latest {
			utils.Fatalf("Checkpoint is too old")
		}
		if cindex == latest && (latest != 0 || h.Uint64() != 0) {
			utils.Fatalf("Stale checkpoint, latest registered %d, given %d", latest, cindex)
		}
	}
	var (
		signature string
		signer    string
	)
	// isAdmin checks whether the specified signer is admin.
	isAdmin := func(addr common.Address) error {
		signers, err := oracle.Contract().GetAllAdmin(nil)
		if err != nil {
			return err
		}
		for _, s := range signers {
			if s == addr {
				return nil
			}
		}
		return fmt.Errorf("signer %v is not the admin", addr.Hex())
	}
	// Print to the user the data thy are about to sign
	fmt.Printf("Oracle     => %s\n", address.Hex())
	fmt.Printf("Index %4d => %s\n", cindex, chash.Hex())

	// Sign checkpoint in clef mode.
	signer = ctx.String(signerFlag.Name)

	if !offline {
		if err := isAdmin(common.HexToAddress(signer)); err != nil {
			return err
		}
	}
	clef := newRPCClient(ctx.String(clefURLFlag.Name))
	p := make(map[string]string)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, cindex)
	p["address"] = address.Hex()
	p["message"] = hexutil.Encode(append(buf, chash.Bytes()...))

	fmt.Println("Sending signing request to Clef...")
	if err := clef.Call(&signature, "account_signData", accounts.MimetypeDataWithValidator, signer, p); err != nil {
		utils.Fatalf("Failed to sign checkpoint, err %v", err)
	}
	fmt.Printf("Signer     => %s\n", signer)
	fmt.Printf("Signature  => %s\n", signature)
	return nil
}

// sighash calculates the hash of the data to sign for the checkpoint oracle.
func sighash(index uint64, oracle common.Address, hash common.Hash) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, index)

	data := append([]byte{0x19, 0x00}, append(oracle[:], append(buf, hash[:]...)...)...)
	return crypto.Keccak256(data)
}

// ecrecover calculates the sender address from a sighash and signature combo.
func ecrecover(sighash []byte, sig []byte) common.Address {
	sig[64] -= 27
	defer func() { sig[64] += 27 }()

	signer, err := crypto.SigToPub(sighash, sig)
	if err != nil {
		utils.Fatalf("Failed to recover sender from signature %x: %v", sig, err)
	}
	return crypto.PubkeyToAddress(*signer)
}

// 发布寄存器由连接节点生成的指定检查点
//带有授权的私钥。
func publish(ctx *cli.Context) error {
	// Print the checkpoint oracle's current status to make sure we're interacting
	// with the correct network and contract.
	status(ctx)

	// Gather the signatures from the CLI
	var sigs [][]byte
	for _, sig := range strings.Split(ctx.String(signaturesFlag.Name), ",") {
		trimmed := strings.TrimPrefix(strings.TrimSpace(sig), "0x")
		if len(trimmed) != 130 {
			utils.Fatalf("Invalid signature in --signature: '%s'", trimmed)
		} else {
			sigs = append(sigs, common.Hex2Bytes(trimmed))
		}
	}
	// Retrieve the checkpoint we want to sign to sort the signatures
	var (
		client       = newRPCClient(ctx.String(nodeURLFlag.Name))
		addr, oracle = newContract(client)
		checkpoint   = getCheckpoint(ctx, client)
		sighash      = sighash(checkpoint.SectionIndex, addr, checkpoint.Hash())
	)
	for i := 0; i < len(sigs); i++ {
		for j := i + 1; j < len(sigs); j++ {
			signerA := ecrecover(sighash, sigs[i])
			signerB := ecrecover(sighash, sigs[j])
			if bytes.Compare(signerA.Bytes(), signerB.Bytes()) > 0 {
				sigs[i], sigs[j] = sigs[j], sigs[i]
			}
		}
	}
	// Retrieve recent header info to protect replay attack
	reqCtx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFn()

	head, err := ethclient.NewClient(client).HeaderByNumber(reqCtx, nil)
	if err != nil {
		return err
	}
	num := head.Number.Uint64()
	recent, err := ethclient.NewClient(client).HeaderByNumber(reqCtx, big.NewInt(int64(num-128)))
	if err != nil {
		return err
	}
	// Print a summary of the operation that's going to be performed
	fmt.Printf("Publishing %d => %s:\n\n", checkpoint.SectionIndex, checkpoint.Hash().Hex())
	for i, sig := range sigs {
		fmt.Printf("Signer %d => %s\n", i+1, ecrecover(sighash, sig).Hex())
	}
	fmt.Println()
	fmt.Printf("Sentry number => %d\nSentry hash   => %s\n", recent.Number, recent.Hash().Hex())

	// Publish the checkpoint into the oracle
	fmt.Println("Sending publish request to Clef...")
	tx, err := oracle.RegisterCheckpoint(newClefSigner(ctx), checkpoint.SectionIndex, checkpoint.Hash().Bytes(), recent.Number, recent.Hash(), sigs)
	if err != nil {
		utils.Fatalf("Register contract failed %v", err)
	}
	log.Info("Successfully registered checkpoint", "tx", tx.Hash().Hex())
	return nil
}
