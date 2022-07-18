// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// EVMLOGGER用于从EVM事务中收集执行跟踪
// 执行。caftreastate均为VM的每个步骤都调用
//当前的VM状态。
//请注意，参考类型是实际的VM数据结构；制作副本
//如果您需要将它们保留在当前电话之外。
type EVMLogger interface {
	// Transaction level
	CaptureTxStart(gasLimit uint64)
	CaptureTxEnd(restGas uint64)
	// Top call frame
	CaptureStart(env *EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int)
	CaptureEnd(output []byte, gasUsed uint64, t time.Duration, err error)
	// Rest of call frames
	CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int)
	CaptureExit(output []byte, gasUsed uint64, err error)
	// Opcode level
	CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)
	CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error)
}
