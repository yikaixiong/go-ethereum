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

// Checkpoint-Admin是一个可用于查询检查点信息的实用程序
//并将稳定检查点注册到Oracle合同中。
package main

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var (
	// Git SHA1 commit hash of the release (set via linker flags)
	gitCommit = ""
	gitDate   = ""

	app *cli.App
)

// 初始化
func init() {
	app = flags.NewApp(gitCommit, gitDate, "ethereum checkpoint helper tool")
	app.Commands = []*cli.Command{
		commandStatus,
		commandDeploy,
		commandSign,
		commandPublish,
	}
	app.Flags = []cli.Flag{
		oracleFlag,
		nodeURLFlag,
	}
}

// 常用的命令行标志。
var (
	indexFlag = &cli.Int64Flag{
		Name:  "index",
		Usage: "Checkpoint index (query latest from remote node if not specified)",
	}
	hashFlag = &cli.StringFlag{
		Name:  "hash",
		Usage: "Checkpoint hash (query latest from remote node if not specified)",
	}
	oracleFlag = &cli.StringFlag{
		Name:  "oracle",
		Usage: "Checkpoint oracle address (query from remote node if not specified)",
	}
	thresholdFlag = &cli.Int64Flag{
		Name:  "threshold",
		Usage: "Minimal number of signatures required to approve a checkpoint",
	}
	nodeURLFlag = &cli.StringFlag{
		Name:  "rpc",
		Value: "http://localhost:8545",
		Usage: "The rpc endpoint of a local or remote geth node",
	}
	clefURLFlag = &cli.StringFlag{
		Name:  "clef",
		Value: "http://localhost:8550",
		Usage: "The rpc endpoint of clef",
	}
	signerFlag = &cli.StringFlag{
		Name:  "signer",
		Usage: "Signer address for clef signing",
	}
	signersFlag = &cli.StringFlag{
		Name:  "signers",
		Usage: "Comma separated accounts of trusted checkpoint signers",
	}
	signaturesFlag = &cli.StringFlag{
		Name:  "signatures",
		Usage: "Comma separated checkpoint signatures to submit",
	}
)

// 启动主函数
func main() {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	fdlimit.Raise(2048)

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
