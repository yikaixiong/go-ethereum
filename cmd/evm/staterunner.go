//版权所有2017年作者
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
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/tests"

	"github.com/urfave/cli/v2"
)

var stateTestCommand = &cli.Command{
	Action:    stateTestCmd,
	Name:      "statetest",
	Usage:     "executes the given state tests",
	ArgsUsage: "<file>",
}

// StatetestResult contains the execution status after running a state test, any
// error that might have occurred and a dump of the final state if requested.
type StatetestResult struct {
	Name  string      `json:"name"`
	Pass  bool        `json:"pass"`
	Fork  string      `json:"fork"`
	Error string      `json:"error,omitempty"`
	State *state.Dump `json:"state,omitempty"`
}

func stateTestCmd(ctx *cli.Context) error {
	if len(ctx.Args().First()) == 0 {
		return errors.New("path-to-test argument required")
	}
	// Configure the go-ethereum logger
	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.Lvl(ctx.Int(VerbosityFlag.Name)))
	log.Root().SetHandler(glogger)

	// Configure the EVM logger
	config := &logger.Config{
		EnableMemory:     !ctx.Bool(DisableMemoryFlag.Name),
		DisableStack:     ctx.Bool(DisableStackFlag.Name),
		DisableStorage:   ctx.Bool(DisableStorageFlag.Name),
		EnableReturnData: !ctx.Bool(DisableReturnDataFlag.Name),
	}
	var (
		tracer   vm.EVMLogger
		debugger *logger.StructLogger
	)
	switch {
	case ctx.Bool(MachineFlag.Name):
		tracer = logger.NewJSONLogger(config, os.Stderr)

	case ctx.Bool(DebugFlag.Name):
		debugger = logger.NewStructLogger(config)
		tracer = debugger

	default:
		debugger = logger.NewStructLogger(config)
	}
	// Load the test content from the input file
	src, err := os.ReadFile(ctx.Args().First())
	if err != nil {
		return err
	}
	var tests map[string]tests.StateTest
	if err = json.Unmarshal(src, &tests); err != nil {
		return err
	}
	// Iterate over all the tests, run them and aggregate the results
	cfg := vm.Config{
		Tracer: tracer,
		Debug:  ctx.Bool(DebugFlag.Name) || ctx.Bool(MachineFlag.Name),
	}
	results := make([]StatetestResult, 0, len(tests))
	for key, test := range tests {
		for _, st := range test.Subtests() {
			// Run the test and aggregate the result
			result := &StatetestResult{Name: key, Fork: st.Fork, Pass: true}
			_, s, err := test.Run(st, cfg, false)
			// print state root for evmlab tracing
			if ctx.Bool(MachineFlag.Name) && s != nil {
				fmt.Fprintf(os.Stderr, "{\"stateRoot\": \"%x\"}\n", s.IntermediateRoot(false))
			}
			if err != nil {
				// Test failed, mark as so and dump any state to aid debugging
				result.Pass, result.Error = false, err.Error()
				if ctx.Bool(DumpFlag.Name) && s != nil {
					dump := s.RawDump(nil)
					result.State = &dump
				}
			}

			results = append(results, *result)

			// Print any structured logs collected
			if ctx.Bool(DebugFlag.Name) {
				if debugger != nil {
					fmt.Fprintln(os.Stderr, "#### TRACE ####")
					logger.WriteTrace(os.Stderr, debugger.StructLogs())
				}
			}
		}
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
	return nil
}
