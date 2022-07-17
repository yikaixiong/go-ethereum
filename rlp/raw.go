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

package rlp

import (
	"io"
	"reflect"
)

// RAWVALUE代表编码的RLP值，可用于延迟
// RLP解码或预先计算编码。请注意，解码器确实
//不要验证RAWVALUES的内容是否有效RLP。
type RawValue []byte

var rawValueType = reflect.TypeOf(RawValue{})

// 列表size返回带有给定的RLP列表的编码大小
//内容大小。
func ListSize(contentSize uint64) uint64 {
	return uint64(headsize(contentSize)) + contentSize
}

// intsize返回整数x的编码大小。
func IntSize(x uint64) int {
	if x < 0x80 {
		return 1
	}
	return 1 + intsize(x)
}

//拆分返回第一个RLP值的内容和任何
//值之后的字节作为b的子词。
func Split(b []byte) (k Kind, content, rest []byte, err error) {
	k, ts, cs, err := readKind(b)
	if err != nil {
		return 0, nil, b, err
	}
	return k, b[ts : ts+cs], b[ts+cs:], nil
}

// 将B分配到RLP字符串的内容中
//和字符串后的任何剩余字节。
func SplitString(b []byte) (content, rest []byte, err error) {
	k, content, rest, err := Split(b)
	if err != nil {
		return nil, b, err
	}
	if k == List {
		return nil, b, ErrExpectedString
	}
	return content, rest, nil
}

// splituint64在b的开头解码一个整数。
//它还返回“休息”整数之后的剩余数据。
func SplitUint64(b []byte) (x uint64, rest []byte, err error) {
	content, rest, err := SplitString(b)
	if err != nil {
		return 0, b, err
	}
	switch {
	case len(content) == 0:
		return 0, rest, nil
	case len(content) == 1:
		if content[0] == 0 {
			return 0, b, ErrCanonInt
		}
		return uint64(content[0]), rest, nil
	case len(content) > 8:
		return 0, b, errUintOverflow
	default:
		x, err = readSize(content, byte(len(content)))
		if err != nil {
			return 0, b, ErrCanonInt
		}
		return x, rest, nil
	}
}

// SplitList将B分配到列表的内容和剩余的任何内容
//列表之后的字节。
func SplitList(b []byte) (content, rest []byte, err error) {
	k, content, rest, err := Split(b)
	if err != nil {
		return nil, b, err
	}
	if k != List {
		return nil, b, ErrExpectedList
	}
	return content, rest, nil
}

// 计数值计算b中编码值的数量。
func CountValues(b []byte) (int, error) {
	i := 0
	for ; len(b) > 0; i++ {
		_, tagsize, size, err := readKind(b)
		if err != nil {
			return 0, err
		}
		b = b[tagsize+size:]
	}
	return i, nil
}

func readKind(buf []byte) (k Kind, tagsize, contentsize uint64, err error) {
	if len(buf) == 0 {
		return 0, 0, 0, io.ErrUnexpectedEOF
	}
	b := buf[0]
	switch {
	case b < 0x80:
		k = Byte
		tagsize = 0
		contentsize = 1
	case b < 0xB8:
		k = String
		tagsize = 1
		contentsize = uint64(b - 0x80)
		// Reject strings that should've been single bytes.
		if contentsize == 1 && len(buf) > 1 && buf[1] < 128 {
			return 0, 0, 0, ErrCanonSize
		}
	case b < 0xC0:
		k = String
		tagsize = uint64(b-0xB7) + 1
		contentsize, err = readSize(buf[1:], b-0xB7)
	case b < 0xF8:
		k = List
		tagsize = 1
		contentsize = uint64(b - 0xC0)
	default:
		k = List
		tagsize = uint64(b-0xF7) + 1
		contentsize, err = readSize(buf[1:], b-0xF7)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	// Reject values larger than the input slice.
	if contentsize > uint64(len(buf))-tagsize {
		return 0, 0, 0, ErrValueTooLarge
	}
	return k, tagsize, contentsize, err
}

func readSize(b []byte, slen byte) (uint64, error) {
	if int(slen) > len(b) {
		return 0, io.ErrUnexpectedEOF
	}
	var s uint64
	switch slen {
	case 1:
		s = uint64(b[0])
	case 2:
		s = uint64(b[0])<<8 | uint64(b[1])
	case 3:
		s = uint64(b[0])<<16 | uint64(b[1])<<8 | uint64(b[2])
	case 4:
		s = uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	case 5:
		s = uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
	case 6:
		s = uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 | uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5])
	case 7:
		s = uint64(b[0])<<48 | uint64(b[1])<<40 | uint64(b[2])<<32 | uint64(b[3])<<24 | uint64(b[4])<<16 | uint64(b[5])<<8 | uint64(b[6])
	case 8:
		s = uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 | uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
	}
	// Reject sizes < 56 (shouldn't have separate size) and sizes with
	// leading zero bytes.
	if s < 56 || b[0] == 0 {
		return 0, ErrCanonSize
	}
	return s, nil
}

//appenduint64将i的RLP编码附加到b，并返回结果切片。
func AppendUint64(b []byte, i uint64) []byte {
	if i == 0 {
		return append(b, 0x80)
	} else if i < 128 {
		return append(b, byte(i))
	}
	switch {
	case i < (1 << 8):
		return append(b, 0x81, byte(i))
	case i < (1 << 16):
		return append(b, 0x82,
			byte(i>>8),
			byte(i),
		)
	case i < (1 << 24):
		return append(b, 0x83,
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)
	case i < (1 << 32):
		return append(b, 0x84,
			byte(i>>24),
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)
	case i < (1 << 40):
		return append(b, 0x85,
			byte(i>>32),
			byte(i>>24),
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)

	case i < (1 << 48):
		return append(b, 0x86,
			byte(i>>40),
			byte(i>>32),
			byte(i>>24),
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)
	case i < (1 << 56):
		return append(b, 0x87,
			byte(i>>48),
			byte(i>>40),
			byte(i>>32),
			byte(i>>24),
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)

	default:
		return append(b, 0x88,
			byte(i>>56),
			byte(i>>48),
			byte(i>>40),
			byte(i>>32),
			byte(i>>24),
			byte(i>>16),
			byte(i>>8),
			byte(i),
		)
	}
}
