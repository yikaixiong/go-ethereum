//版权所有2014年作者
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

// EVM执行EVM代码段。
package main

import (
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/cmd/evm/internal/t8ntool"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/urfave/cli/v2"
)

var (
	gitCommit = "" // Git SHA1 commit hash of the release (set via linker flags)
	gitDate   = ""

	app = flags.NewApp(gitCommit, gitDate, "the evm command line interface")
)

var (
	DebugFlag = &cli.BoolFlag{
		Name:  "debug",
		Usage: "output full trace logs",
	}
	MemProfileFlag = &cli.StringFlag{
		Name:  "memprofile",
		Usage: "creates a memory profile at the given path",
	}
	CPUProfileFlag = &cli.StringFlag{
		Name:  "cpuprofile",
		Usage: "creates a CPU profile at the given path",
	}
	StatDumpFlag = &cli.BoolFlag{
		Name:  "statdump",
		Usage: "displays stack and heap memory information",
	}
	CodeFlag = &cli.StringFlag{
		Name:  "code",
		Usage: "EVM code",
	}
	CodeFileFlag = &cli.StringFlag{
		Name:  "codefile",
		Usage: "File containing EVM code. If '-' is specified, code is read from stdin ",
	}
	GasFlag = &cli.Uint64Flag{
		Name:  "gas",
		Usage: "gas limit for the evm",
		Value: 10000000000,
	}
	PriceFlag = &flags.BigFlag{
		Name:  "price",
		Usage: "price set for the evm",
		Value: new(big.Int),
	}
	ValueFlag = &flags.BigFlag{
		Name:  "value",
		Usage: "value set for the evm",
		Value: new(big.Int),
	}
	DumpFlag = &cli.BoolFlag{
		Name:  "dump",
		Usage: "dumps the state after the run",
	}
	InputFlag = &cli.StringFlag{
		Name:  "input",
		Usage: "input for the EVM",
	}
	InputFileFlag = &cli.StringFlag{
		Name:  "inputfile",
		Usage: "file containing input for the EVM",
	}
	VerbosityFlag = &cli.IntFlag{
		Name:  "verbosity",
		Usage: "sets the verbosity level",
	}
	BenchFlag = &cli.BoolFlag{
		Name:  "bench",
		Usage: "benchmark the execution",
	}
	CreateFlag = &cli.BoolFlag{
		Name:  "create",
		Usage: "indicates the action should be create rather than call",
	}
	GenesisFlag = &cli.StringFlag{
		Name:  "prestate",
		Usage: "JSON file with prestate (genesis) config",
	}
	MachineFlag = &cli.BoolFlag{
		Name:  "json",
		Usage: "output trace logs in machine readable format (json)",
	}
	SenderFlag = &cli.StringFlag{
		Name:  "sender",
		Usage: "The transaction origin",
	}
	ReceiverFlag = &cli.StringFlag{
		Name:  "receiver",
		Usage: "The transaction receiver (execution context)",
	}
	DisableMemoryFlag = &cli.BoolFlag{
		Name:  "nomemory",
		Value: true,
		Usage: "disable memory output",
	}
	DisableStackFlag = &cli.BoolFlag{
		Name:  "nostack",
		Usage: "disable stack output",
	}
	DisableStorageFlag = &cli.BoolFlag{
		Name:  "nostorage",
		Usage: "disable storage output",
	}
	DisableReturnDataFlag = &cli.BoolFlag{
		Name:  "noreturndata",
		Value: true,
		Usage: "enable return data output",
	}
)

var stateTransitionCommand = &cli.Command{
	Name:    "transition",
	Aliases: []string{"t8n"},
	Usage:   "executes a full state transition",
	Action:  t8ntool.Transition,
	Flags: []cli.Flag{
		t8ntool.TraceFlag,
		t8ntool.TraceDisableMemoryFlag,
		t8ntool.TraceEnableMemoryFlag,
		t8ntool.TraceDisableStackFlag,
		t8ntool.TraceDisableReturnDataFlag,
		t8ntool.TraceEnableReturnDataFlag,
		t8ntool.OutputBasedir,
		t8ntool.OutputAllocFlag,
		t8ntool.OutputResultFlag,
		t8ntool.OutputBodyFlag,
		t8ntool.InputAllocFlag,
		t8ntool.InputEnvFlag,
		t8ntool.InputTxsFlag,
		t8ntool.ForknameFlag,
		t8ntool.ChainIDFlag,
		t8ntool.RewardFlag,
		t8ntool.VerbosityFlag,
	},
}

var transactionCommand = &cli.Command{
	Name:    "transaction",
	Aliases: []string{"t9n"},
	Usage:   "performs transaction validation",
	Action:  t8ntool.Transaction,
	Flags: []cli.Flag{
		t8ntool.InputTxsFlag,
		t8ntool.ChainIDFlag,
		t8ntool.ForknameFlag,
		t8ntool.VerbosityFlag,
	},
}

var blockBuilderCommand = &cli.Command{
	Name:    "block-builder",
	Aliases: []string{"b11r"},
	Usage:   "builds a block",
	Action:  t8ntool.BuildBlock,
	Flags: []cli.Flag{
		t8ntool.OutputBasedir,
		t8ntool.OutputBlockFlag,
		t8ntool.InputHeaderFlag,
		t8ntool.InputOmmersFlag,
		t8ntool.InputTxsRlpFlag,
		t8ntool.SealCliqueFlag,
		t8ntool.SealEthashFlag,
		t8ntool.SealEthashDirFlag,
		t8ntool.SealEthashModeFlag,
		t8ntool.VerbosityFlag,
	},
}

// 初始化
func init() {
	app.Flags = []cli.Flag{
		BenchFlag,
		CreateFlag,
		DebugFlag,
		VerbosityFlag,
		CodeFlag,
		CodeFileFlag,
		GasFlag,
		PriceFlag,
		ValueFlag,
		DumpFlag,
		InputFlag,
		InputFileFlag,
		MemProfileFlag,
		CPUProfileFlag,
		StatDumpFlag,
		GenesisFlag,
		MachineFlag,
		SenderFlag,
		ReceiverFlag,
		DisableMemoryFlag,
		DisableStackFlag,
		DisableStorageFlag,
		DisableReturnDataFlag,
	}
	app.Commands = []*cli.Command{
		compileCommand,
		disasmCommand,
		runCommand,
		stateTestCommand,
		stateTransitionCommand,
		transactionCommand,
		blockBuilderCommand,
	}
}

// 主函数
func main() {
	if err := app.Run(os.Args); err != nil {
		code := 1
		if ec, ok := err.(*t8ntool.NumberedError); ok {
			code = ec.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(code)
	}
}
