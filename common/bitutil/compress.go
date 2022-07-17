//版权所有2017年作者
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


// TODO: bit字节比较函数
package bitutil

import "errors"

var (
	// errMissingData is returned from decompression if the byte referenced by
	// the bitset header overflows the input data.
	errMissingData = errors.New("missing bytes on input")

	// errUnreferencedData is returned from decompression if not all bytes were used
	// up from the input data after decompressing it.
	errUnreferencedData = errors.New("extra bytes on input")

	// errExceededTarget is returned from decompression if the bitset header has
	// more bits defined than the number of target buffer space available.
	errExceededTarget = errors.New("target data size exceeded")

	// errZeroContent is returned from decompression if a data byte referenced in
	// the bitset header is actually a zero byte.
	errZeroContent = errors.New("zero byte in input content")
)

//由压缩型和解压缩词实现的压缩算法是
//针对包含大量零字节的稀疏输入数据进行了优化。减压
//需要了解解压缩的数据长度。
//
//压缩工作如下：
//
//如果数据仅包含零，则
// compressbytes（data）== nil
//否则如果Len（data）<= 1，
// compressbytes（data）==数据
//   否则：
// compressbytes（data）== append（compressbytes（nonzerobitset（data）），nonzerobytes（data）...）
//       在哪里
// nonzerobitset（数据）是Len（数据）位（MSB）的位矢量：
// nonzerobitset（data）[i/8] &&（1 <<（7-i％8））！= 0如果数据[i]！= 0
// len（nonzerobitset（data））==（len（data）+7）/8
// nonzerobytes（数据）以相同顺序包含数据的非零字节

//压缩BYSTES根据稀疏位置压缩输入字节切片
//表示算法。如果结果大于原始输入，则不
//压缩完成。
func CompressBytes(data []byte) []byte {
	if out := bitsetEncodeBytes(data); len(out) < len(data) {
		return out
	}
	cpy := make([]byte, len(data))
	copy(cpy, data)
	return cpy
}

// BitsetEncodeBytes根据稀疏压缩输入字节切片
// BITSET表示算法。
func bitsetEncodeBytes(data []byte) []byte {
	// Empty slices get compressed to nil
	if len(data) == 0 {
		return nil
	}
	// One byte slices compress to nil or retain the single byte
	if len(data) == 1 {
		if data[0] == 0 {
			return nil
		}
		return data
	}
	// Calculate the bitset of set bytes, and gather the non-zero bytes
	nonZeroBitset := make([]byte, (len(data)+7)/8)
	nonZeroBytes := make([]byte, 0, len(data))

	for i, b := range data {
		if b != 0 {
			nonZeroBytes = append(nonZeroBytes, b)
			nonZeroBitset[i/8] |= 1 << byte(7-i%8)
		}
	}
	if len(nonZeroBytes) == 0 {
		return nil
	}
	return append(bitsetEncodeBytes(nonZeroBitset), nonZeroBytes...)
}

// 解压缩BYTES用已知的目标大小解压缩数据。如果输入数据
//匹配目标的大小，这意味着在第一个中没有进行压缩
// 地方。
func DecompressBytes(data []byte, target int) ([]byte, error) {
	if len(data) > target {
		return nil, errExceededTarget
	}
	if len(data) == target {
		cpy := make([]byte, len(data))
		copy(cpy, data)
		return cpy, nil
	}
	return bitsetDecodeBytes(data, target)
}

// BITSETDECODEBYTES用已知的目标大小解压缩数据。
func bitsetDecodeBytes(data []byte, target int) ([]byte, error) {
	out, size, err := bitsetDecodePartialBytes(data, target)
	if err != nil {
		return nil, err
	}
	if size != len(data) {
		return nil, errUnreferencedData
	}
	return out, nil
}

// bitsetDecodePartialBytes decompresses data with a known target size, but does
// not enforce consuming all the input bytes. In addition to the decompressed
// output, the function returns the length of compressed input data corresponding
// to the output as the input slice may be longer.
func bitsetDecodePartialBytes(data []byte, target int) ([]byte, int, error) {
	// Sanity check 0 targets to avoid infinite recursion
	if target == 0 {
		return nil, 0, nil
	}
	// Handle the zero and single byte corner cases
	decomp := make([]byte, target)
	if len(data) == 0 {
		return decomp, 0, nil
	}
	if target == 1 {
		decomp[0] = data[0] // copy to avoid referencing the input slice
		if data[0] != 0 {
			return decomp, 1, nil
		}
		return decomp, 0, nil
	}
	// Decompress the bitset of set bytes and distribute the non zero bytes
	nonZeroBitset, ptr, err := bitsetDecodePartialBytes(data, (target+7)/8)
	if err != nil {
		return nil, ptr, err
	}
	for i := 0; i < 8*len(nonZeroBitset); i++ {
		if nonZeroBitset[i/8]&(1<<byte(7-i%8)) != 0 {
			// Make sure we have enough data to push into the correct slot
			if ptr >= len(data) {
				return nil, 0, errMissingData
			}
			if i >= len(decomp) {
				return nil, 0, errExceededTarget
			}
			// Make sure the data is valid and push into the slot
			if data[ptr] == 0 {
				return nil, 0, errZeroContent
			}
			decomp[i] = data[ptr]
			ptr++
		}
	}
	return decomp, ptr, nil
}
