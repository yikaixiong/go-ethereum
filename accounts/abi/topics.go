//版权2018 go-ethereum作者
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
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// 石材台词将过滤器查询参数列表转换为过滤主题集。
func MakeTopics(query ...[]interface{}) ([][]common.Hash, error) {
	topics := make([][]common.Hash, len(query))
	for i, filter := range query {
		for _, rule := range filter {
			var topic common.Hash

			// 尝试根据简单类型生成主题
			switch rule := rule.(type) {
			case common.Hash:
				copy(topic[:], rule[:])
			case common.Address:
				copy(topic[common.HashLength-common.AddressLength:], rule[:])
			case *big.Int:
				blob := rule.Bytes()
				copy(topic[common.HashLength-len(blob):], blob)
			case bool:
				if rule {
					topic[common.HashLength-1] = 1
				}
			case int8:
				copy(topic[:], genIntType(int64(rule), 1))
			case int16:
				copy(topic[:], genIntType(int64(rule), 2))
			case int32:
				copy(topic[:], genIntType(int64(rule), 4))
			case int64:
				copy(topic[:], genIntType(rule, 8))
			case uint8:
				blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
				copy(topic[common.HashLength-len(blob):], blob)
			case uint16:
				blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
				copy(topic[common.HashLength-len(blob):], blob)
			case uint32:
				blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
				copy(topic[common.HashLength-len(blob):], blob)
			case uint64:
				blob := new(big.Int).SetUint64(rule).Bytes()
				copy(topic[common.HashLength-len(blob):], blob)
			case string:
				hash := crypto.Keccak256Hash([]byte(rule))
				copy(topic[:], hash[:])
			case []byte:
				hash := crypto.Keccak256Hash(rule)
				copy(topic[:], hash[:])

			default:
				// todo(rjl493456442) according solidity documentation, indexed event
				// parameters that are not value types i.e. arrays and structs are not
				// stored directly but instead a keccak256-hash of an encoding is stored.
				//
				// We only convert stringS and bytes to hash, still need to deal with
				// array(both fixed-size and dynamic-size) and struct.

				// Attempt to generate the topic from funky types
				val := reflect.ValueOf(rule)
				switch {
				// static byte array
				case val.Kind() == reflect.Array && reflect.TypeOf(rule).Elem().Kind() == reflect.Uint8:
					reflect.Copy(reflect.ValueOf(topic[:val.Len()]), val)
				default:
					return nil, fmt.Errorf("unsupported indexed type: %T", rule)
				}
			}
			topics[i] = append(topics[i], topic)
		}
	}
	return topics, nil
}

func genIntType(rule int64, size uint) []byte {
	var topic [common.HashLength]byte
	if rule < 0 {
		// 如果规则为负，我们需要将其放入两者的补充中。
//扩展到common.hashlength字节。
		topic = [common.HashLength]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	}
	for i := uint(0); i < size; i++ {
		topic[common.HashLength-i-1] = byte(rule >> (i * 8))
	}
	return topic[:]
}

// arteSopics将索引主题字段转换为实际日志字段值。
func ParseTopics(out interface{}, fields Arguments, topics []common.Hash) error {
	return parseTopicWithSetter(fields, topics,
		func(arg Argument, reconstr interface{}) {
			field := reflect.ValueOf(out).Elem().FieldByName(ToCamelCase(arg.Name))
			field.Set(reflect.ValueOf(reconstr))
		})
}

//ParsetopicsIntomap将索引主题字段值对转换为地图键值对。
func ParseTopicsIntoMap(out map[string]interface{}, fields Arguments, topics []common.Hash) error {
	return parseTopicWithSetter(fields, topics,
		func(arg Argument, reconstr interface{}) {
			out[arg.Name] = reconstr
		})
}

// parsetopicWithSetter转换索引主题字段值对，并使用
//提供的设置功能。
//
//注意，由于将动态类型映射到Keccak256，因此无法重建动态类型
//哈希作为主题价值！
func parseTopicWithSetter(fields Arguments, topics []common.Hash, setter func(Argument, interface{})) error {
	// 理智检查字段和主题是否匹配
	if len(fields) != len(topics) {
		return errors.New("topic/field count mismatch")
	}
	// 在所有领域中迭代并从主题中重建它们
	for i, arg := range fields {
		if !arg.Indexed {
			return errors.New("non-indexed field in topic reconstruction")
		}
		var reconstr interface{}
		switch arg.Type.T {
		case TupleTy:
			return errors.New("tuple type in topic reconstruction")
		case StringTy, BytesTy, SliceTy, ArrayTy:
			// Array types (including strings and bytes) have their keccak256 hashes stored in the topic- not a hash
			// whose bytes can be decoded to the actual value- so the best we can do is retrieve that hash
			reconstr = topics[i]
		case FunctionTy:
			if garbage := binary.BigEndian.Uint64(topics[i][0:8]); garbage != 0 {
				return fmt.Errorf("bind: got improperly encoded function type, got %v", topics[i].Bytes())
			}
			var tmp [24]byte
			copy(tmp[:], topics[i][8:32])
			reconstr = tmp
		default:
			var err error
			reconstr, err = toGoType(0, arg.Type, topics[i].Bytes())
			if err != nil {
				return err
			}
		}
		// Use the setter function to store the value
		setter(arg, reconstr)
	}

	return nil
}
