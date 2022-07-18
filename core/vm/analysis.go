//版权所有2014年作者
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

package vm

const (
	set2BitsMask = uint16(0b11)
	set3BitsMask = uint16(0b111)
	set4BitsMask = uint16(0b1111)
	set5BitsMask = uint16(0b1_1111)
	set6BitsMask = uint16(0b11_1111)
	set7BitsMask = uint16(0b111_1111)
)

// BITVEC是一个位向量，可在程序中映射字节。
//一个不设置的位意味着字节是一个opcode，set位均值
//它的数据（即Pushxx的参数）。
type bitvec []byte

func (bits bitvec) set1(pos uint64) {
	bits[pos/8] |= 1 << (pos % 8)
}

func (bits bitvec) setN(flag uint16, pos uint64) {
	a := flag << (pos % 8)
	bits[pos/8] |= byte(a)
	if b := byte(a >> 8); b != 0 {
		bits[pos/8+1] = b
	}
}

func (bits bitvec) set8(pos uint64) {
	a := byte(0xFF << (pos % 8))
	bits[pos/8] |= a
	bits[pos/8+1] = ^a
}

func (bits bitvec) set16(pos uint64) {
	a := byte(0xFF << (pos % 8))
	bits[pos/8] |= a
	bits[pos/8+1] = 0xFF
	bits[pos/8+2] = ^a
}

// CODESEGENG检查该位置是否在代码段中。
func (bits *bitvec) codeSegment(pos uint64) bool {
	return (((*bits)[pos/8] >> (pos % 8)) & 1) == 0
}

// codeBitmap collects data locations in code.
func codeBitmap(code []byte) bitvec {
	// 位图比必要的4个字节长，以防代码
//以push32结束，该算法将零推到
// BITVECTOR在实际代码的范围之外。
	bits := make(bitvec, len(code)/8+1+4)
	return codeBitmapInternal(code, bits)
}

// codeBitmapInternal is the internal implementation of codeBitmap.
// It exists for the purpose of being able to run benchmark tests
// without dynamic allocations affecting the results.
func codeBitmapInternal(code, bits bitvec) bitvec {
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])
		pc++
		if int8(op) < int8(PUSH1) { // If not PUSH (the int8(op) > int(PUSH32) is always false).
			continue
		}
		numbits := op - PUSH1 + 1
		if numbits >= 8 {
			for ; numbits >= 16; numbits -= 16 {
				bits.set16(pc)
				pc += 16
			}
			for ; numbits >= 8; numbits -= 8 {
				bits.set8(pc)
				pc += 8
			}
		}
		switch numbits {
		case 1:
			bits.set1(pc)
			pc += 1
		case 2:
			bits.setN(set2BitsMask, pc)
			pc += 2
		case 3:
			bits.setN(set3BitsMask, pc)
			pc += 3
		case 4:
			bits.setN(set4BitsMask, pc)
			pc += 4
		case 5:
			bits.setN(set5BitsMask, pc)
			pc += 5
		case 6:
			bits.setN(set6BitsMask, pc)
			pc += 6
		case 7:
			bits.setN(set7BitsMask, pc)
			pc += 7
		}
	}
	return bits
}
