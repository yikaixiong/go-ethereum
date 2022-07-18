//版权2022 The Go-Ethereum作者
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
package beacon

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

// EngineAPIError is a standardized error message between consensus and execution
// clients, also containing any custom error message Geth might include.
type EngineAPIError struct {
	code int
	msg  string
	err  error
}

func (e *EngineAPIError) ErrorCode() int { return e.code }
func (e *EngineAPIError) Error() string  { return e.msg }
func (e *EngineAPIError) ErrorData() interface{} {
	if e.err == nil {
		return nil
	}
	return struct {
		Error string `json:"err"`
	}{e.err.Error()}
}

// With returns a copy of the error with a new embedded custom data field.
func (e *EngineAPIError) With(err error) *EngineAPIError {
	return &EngineAPIError{
		code: e.code,
		msg:  e.msg,
		err:  err,
	}
}

var (
	_ rpc.Error     = new(EngineAPIError)
	_ rpc.DataError = new(EngineAPIError)
)

var (
	// VALID is returned by the engine API in the following calls:
	//   - newPayloadV1:       if the payload was already known or was just validated and executed
	//   - forkchoiceUpdateV1: if the chain accepted the reorg (might ignore if it's stale)
	VALID = "VALID"

	// INVALID is returned by the engine API in the following calls:
	//   - newPayloadV1:       if the payload failed to execute on top of the local chain
	//   - forkchoiceUpdateV1: if the new head is unknown, pre-merge, or reorg to it fails
	INVALID = "INVALID"

	// SYNCING is returned by the engine API in the following calls:
	//   - newPayloadV1:       if the payload was accepted on top of an active sync
	//   - forkchoiceUpdateV1: if the new head was seen before, but not part of the chain
	SYNCING = "SYNCING"

	// ACCEPTED is returned by the engine API in the following calls:
	//   - newPayloadV1: if the payload was accepted, but not processed (side chain)
	ACCEPTED = "ACCEPTED"

	INVALIDBLOCKHASH = "INVALID_BLOCK_HASH"

	GenericServerError       = &EngineAPIError{code: -32000, msg: "Server error"}
	UnknownPayload           = &EngineAPIError{code: -38001, msg: "Unknown payload"}
	InvalidForkChoiceState   = &EngineAPIError{code: -38002, msg: "Invalid forkchoice state"}
	InvalidPayloadAttributes = &EngineAPIError{code: -38003, msg: "Invalid payload attributes"}

	STATUS_INVALID         = ForkChoiceResponse{PayloadStatus: PayloadStatusV1{Status: INVALID}, PayloadID: nil}
	STATUS_SYNCING         = ForkChoiceResponse{PayloadStatus: PayloadStatusV1{Status: SYNCING}, PayloadID: nil}
	INVALID_TERMINAL_BLOCK = PayloadStatusV1{Status: INVALID, LatestValidHash: &common.Hash{}}
)
