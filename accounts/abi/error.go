//版权所有2016年作者
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
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Error struct {
	Name   string
	Inputs Arguments
	str    string

	// Sig contains the string signature according to the ABI spec.
	// e.g.	 error foo(uint32 a, int b) = "foo(uint32,int256)"
	// Please note that "int" is substitute for its canonical representation "int256"
	Sig string

	// ID returns the canonical representation of the error's signature used by the
	// abi definition to identify event names and types.
	ID common.Hash
}

func NewError(name string, inputs Arguments) Error {
	// sanitize inputs to remove inputs without names
	// and precompute string and sig representation.
	names := make([]string, len(inputs))
	types := make([]string, len(inputs))
	for i, input := range inputs {
		if input.Name == "" {
			inputs[i] = Argument{
				Name:    fmt.Sprintf("arg%d", i),
				Indexed: input.Indexed,
				Type:    input.Type,
			}
		} else {
			inputs[i] = input
		}
		// string representation
		names[i] = fmt.Sprintf("%v %v", input.Type, inputs[i].Name)
		if input.Indexed {
			names[i] = fmt.Sprintf("%v indexed %v", input.Type, inputs[i].Name)
		}
		// sig representation
		types[i] = input.Type.String()
	}

	str := fmt.Sprintf("error %v(%v)", name, strings.Join(names, ", "))
	sig := fmt.Sprintf("%v(%v)", name, strings.Join(types, ","))
	id := common.BytesToHash(crypto.Keccak256([]byte(sig)))

	return Error{
		Name:   name,
		Inputs: inputs,
		str:    str,
		Sig:    sig,
		ID:     id,
	}
}

func (e *Error) String() string {
	return e.str
}

func (e *Error) Unpack(data []byte) (interface{}, error) {
	if len(data) < 4 {
		return "", errors.New("invalid data for unpacking")
	}
	if !bytes.Equal(data[:4], e.ID[:4]) {
		return "", errors.New("invalid data for unpacking")
	}
	return e.Inputs.Unpack(data[4:])
}
