//版权所有2015年作者
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
package abi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ABI持有有关合同背景和可用的信息
//可调用方法。它将允许您键入检查函数调用和
//相应地包装数据。
type ABI struct {
	Constructor Method
	Methods     map[string]Method
	Events      map[string]Event
	Errors      map[string]Error

	// 固体v0.6.0中引入的其他“特殊”功能。
	//它与原始默认后回来分开。每个合同
	//只能定义一个后备并接收功能。
	Fallback Method // 请注意，它也用于表示v0.6.0之前的遗产后备
	Receive  Method
}

// JSON返回解析的ABI接口和错误，如果失败。
func JSON(reader io.Reader) (ABI, error) {
	dec := json.NewDecoder(reader)

	var abi ABI
	if err := dec.Decode(&abi); err != nil {
		return ABI{}, err
	}
	return abi, nil
}

// 打包给定的方法名称以符合ABI。方法调用数据
//将由method_id，args0，arg1，... argn组成。方法ID组成
// 4个字节和参数为32个字节。
//方法ID是从该哈希的前4个字节创建的
//方法字符串签名。（签名= baz（uint32，string32））
func (abi ABI) Pack(name string, args ...interface{}) ([]byte, error) {
	// Fetch the ABI of the requested method
	if name == "" {
		// constructor
		arguments, err := abi.Constructor.Inputs.Pack(args...)
		if err != nil {
			return nil, err
		}
		return arguments, nil
	}
	method, exist := abi.Methods[name]
	if !exist {
		return nil, fmt.Errorf("method '%s' not found", name)
	}
	arguments, err := method.Inputs.Pack(args...)
	if err != nil {
		return nil, err
	}
	// 如果不是构造函数，也打包了方法ID并返回
	return append(method.ID, arguments...), nil
}

func (abi ABI) getArguments(name string, data []byte) (Arguments, error) {
	// 由于不能与合同和事件发生命名碰撞，因此
//我们需要确定我们是在调用方法还是事件
	var args Arguments
	if method, ok := abi.Methods[name]; ok {
		if len(data)%32 != 0 {
			return nil, fmt.Errorf("abi: improperly formatted output: %s - Bytes: [%+v]", string(data), data)
		}
		args = method.Outputs
	}
	if event, ok := abi.Events[name]; ok {
		args = event.Inputs
	}
	if args == nil {
		return nil, errors.New("abi: could not locate named method or event")
	}
	return args, nil
}

// 根据ABI规范解开输出的包装。
func (abi ABI) Unpack(name string, data []byte) ([]interface{}, error) {
	args, err := abi.getArguments(name, data)
	if err != nil {
		return nil, err
	}
	return args.Unpack(data)
}

// 根据ABI规范，UncackIntoInterface解开V中的输出。
//它执行附加副本。如果您想打开一个包装，请仅使用
//不严格符合ABI结构的结构（例如，还有其他参数）
func (abi ABI) UnpackIntoInterface(v interface{}, name string, data []byte) error {
	args, err := abi.getArguments(name, data)
	if err != nil {
		return err
	}
	unpacked, err := args.Unpack(data)
	if err != nil {
		return err
	}
	return args.Copy(v, unpacked)
}

// UnwackIntomap将日志打开到提供的映射[String]接口{}中。
func (abi ABI) UnpackIntoMap(v map[string]interface{}, name string, data []byte) (err error) {
	args, err := abi.getArguments(name, data)
	if err != nil {
		return err
	}
	return args.UnpackIntoMap(v, data)
}

// Unmarshaljson实现JSON.UNMARMALSHALER接口。
func (abi *ABI) UnmarshalJSON(data []byte) error {
	var fields []struct {
		Type    string
		Name    string
		Inputs  []Argument
		Outputs []Argument

		// 状态指示灯可以：“纯”，“查看”，
//“不可付款”或“应付”。
		StateMutability string

		// Deprecated Status indicators, but removed in v0.6.0.
		Constant bool // True if function is either pure or view
		Payable  bool // True if function is payable

		// Event relevant indicator represents the event is
		// declared as anonymous.
		Anonymous bool
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	abi.Methods = make(map[string]Method)
	abi.Events = make(map[string]Event)
	abi.Errors = make(map[string]Error)
	for _, field := range fields {
		switch field.Type {
		case "constructor":
			abi.Constructor = NewMethod("", "", Constructor, field.StateMutability, field.Constant, field.Payable, field.Inputs, nil)
		case "function":
			name := ResolveNameConflict(field.Name, func(s string) bool { _, ok := abi.Methods[s]; return ok })
			abi.Methods[name] = NewMethod(name, field.Name, Function, field.StateMutability, field.Constant, field.Payable, field.Inputs, field.Outputs)
		case "fallback":
			// New introduced function type in v0.6.0, check more detail
			// here https://solidity.readthedocs.io/en/v0.6.0/contracts.html#fallback-function
			if abi.HasFallback() {
				return errors.New("only single fallback is allowed")
			}
			abi.Fallback = NewMethod("", "", Fallback, field.StateMutability, field.Constant, field.Payable, nil, nil)
		case "receive":
			// New introduced function type in v0.6.0, check more detail
			// here https://solidity.readthedocs.io/en/v0.6.0/contracts.html#fallback-function
			if abi.HasReceive() {
				return errors.New("only single receive is allowed")
			}
			if field.StateMutability != "payable" {
				return errors.New("the statemutability of receive can only be payable")
			}
			abi.Receive = NewMethod("", "", Receive, field.StateMutability, field.Constant, field.Payable, nil, nil)
		case "event":
			name := ResolveNameConflict(field.Name, func(s string) bool { _, ok := abi.Events[s]; return ok })
			abi.Events[name] = NewEvent(name, field.Name, field.Anonymous, field.Inputs)
		case "error":
			// Errors cannot be overloaded or overridden but are inherited,
			// no need to resolve the name conflict here.
			abi.Errors[field.Name] = NewError(field.Name, field.Inputs)
		default:
			return fmt.Errorf("abi: could not recognize type %v of field %v", field.Type, field.Name)
		}
	}
	return nil
}

//MethodByid通过4字节ID查找一种方法，
//如果找不到，则返回零。
func (abi *ABI) MethodById(sigdata []byte) (*Method, error) {
	if len(sigdata) < 4 {
		return nil, fmt.Errorf("data too short (%d bytes) for abi method lookup", len(sigdata))
	}
	for _, method := range abi.Methods {
		if bytes.Equal(method.ID, sigdata[:4]) {
			return &method, nil
		}
	}
	return nil, fmt.Errorf("no method with id: %#x", sigdata[:4])
}

// EventByid在其主题上看起来像是一个事件
// ABI，如果没有发现，则将返回零。
func (abi *ABI) EventByID(topic common.Hash) (*Event, error) {
	for _, event := range abi.Events {
		if bytes.Equal(event.ID.Bytes(), topic.Bytes()) {
			return &event, nil
		}
	}
	return nil, fmt.Errorf("no event with id: %#x", topic.Hex())
}

// Hasfallback返回是否包括后备功能。
func (abi *ABI) HasFallback() bool {
	return abi.Fallback.Type == Fallback
}

// HASRECEIVE返回是否包括接收功能。
func (abi *ABI) HasReceive() bool {
	return abi.Receive.Type == Receive
}

// 归还分量是特殊的功能选择器，因为恢复了理由解开包装。
var revertSelector = crypto.Keccak256([]byte("Error(string)"))[:4]

// UnwackReverver解决了ABI编码的还原原因。根据坚固性
//规格https://solity.readthedocs.io/en/latest/control-sstructures.html#revert，
//所提供的恢复原因是对函数的调用的abi编码
//`error（string）`。因此，这是一个特殊的工具。
func UnpackRevert(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("invalid data for unpacking")
	}
	if !bytes.Equal(data[:4], revertSelector) {
		return "", errors.New("invalid data for unpacking")
	}
	typ, _ := NewType("string", "", nil)
	unpacked, err := (Arguments{{Type: typ}}).Unpack(data[4:])
	if err != nil {
		return "", err
	}
	return unpacked[0].(string), nil
}
