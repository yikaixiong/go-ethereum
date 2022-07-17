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

package rlp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/rlp/internal/rlpstruct"
)

//棉布：忽略ST1012 EOL不是错误。

//当当前列表结束时返回EOL
//在流中已达到。
var EOL = errors.New("rlp: end of list")

var (
	ErrExpectedString   = errors.New("rlp: expected String or Byte")
	ErrExpectedList     = errors.New("rlp: expected List")
	ErrCanonInt         = errors.New("rlp: non-canonical integer format")
	ErrCanonSize        = errors.New("rlp: non-canonical size information")
	ErrElemTooLarge     = errors.New("rlp: element is larger than containing list")
	ErrValueTooLarge    = errors.New("rlp: value size exceeds available input length")
	ErrMoreThanOneValue = errors.New("rlp: input contains more than one value")

	// internal errors
	errNotInList     = errors.New("rlp: call of ListEnd outside of any list")
	errNotAtEOL      = errors.New("rlp: call of ListEnd not positioned at EOL")
	errUintOverflow  = errors.New("rlp: uint overflow")
	errNoPointer     = errors.New("rlp: interface given to Decode must be a pointer")
	errDecodeIntoNil = errors.New("rlp: pointer given to Decode must not be nil")

	streamPool = sync.Pool{
		New: func() interface{} { return new(Stream) },
	}
)

// 解码器由需要自定义RLP解码规则或需要解码的类型实现
//进入私人领域。
//
//解码器方法应从给定的流读取一个值。不禁止
//阅读少或更多，但可能会令人困惑。
type Decoder interface {
	DecodeRLP(*Stream) error
}

// 解码解析RLP编码的数据并将结果存储在指向by的值中
// val。有关解码规则，请参阅包级文档。瓦尔必须是
//非尼尔指针。
//
//如果R不实现Bytereader，则DECODE将进行自己的缓冲。
//
//请注意，解码并未为所有读者设置输入限制，并且可能很容易受到影响
//恐慌大小造成巨大的价值。如果您需要输入限制，请使用
//
// newstream（r，limit）.decode（val）
func Decode(r io.Reader, val interface{}) error {
	stream := streamPool.Get().(*Stream)
	defer streamPool.Put(stream)

	stream.Reset(r, 0)
	return stream.Decode(val)
}

// 将B分解RLP数据从B到Val。请参阅包装级文档
//解码规则。输入必须完全包含一个值，并且不包含尾随数据。
func DecodeBytes(b []byte, val interface{}) error {
	r := bytes.NewReader(b)

	stream := streamPool.Get().(*Stream)
	defer streamPool.Put(stream)

	stream.Reset(r, uint64(len(b)))
	if err := stream.Decode(val); err != nil {
		return err
	}
	if r.Len() > 0 {
		return ErrMoreThanOneValue
	}
	return nil
}

type decodeError struct {
	msg string
	typ reflect.Type
	ctx []string
}

func (err *decodeError) Error() string {
	ctx := ""
	if len(err.ctx) > 0 {
		ctx = ", decoding into "
		for i := len(err.ctx) - 1; i >= 0; i-- {
			ctx += err.ctx[i]
		}
	}
	return fmt.Sprintf("rlp: %s for %v%s", err.msg, err.typ, ctx)
}

func wrapStreamError(err error, typ reflect.Type) error {
	switch err {
	case ErrCanonInt:
		return &decodeError{msg: "non-canonical integer (leading zero bytes)", typ: typ}
	case ErrCanonSize:
		return &decodeError{msg: "non-canonical size information", typ: typ}
	case ErrExpectedList:
		return &decodeError{msg: "expected input list", typ: typ}
	case ErrExpectedString:
		return &decodeError{msg: "expected input string or byte", typ: typ}
	case errUintOverflow:
		return &decodeError{msg: "input string too long", typ: typ}
	case errNotAtEOL:
		return &decodeError{msg: "input list has too many elements", typ: typ}
	}
	return err
}

func addErrorContext(err error, ctx string) error {
	if decErr, ok := err.(*decodeError); ok {
		decErr.ctx = append(decErr.ctx, ctx)
	}
	return err
}

var (
	decoderInterface = reflect.TypeOf(new(Decoder)).Elem()
	bigInt           = reflect.TypeOf(big.Int{})
)

func makeDecoder(typ reflect.Type, tags rlpstruct.Tags) (dec decoder, err error) {
	kind := typ.Kind()
	switch {
	case typ == rawValueType:
		return decodeRawValue, nil
	case typ.AssignableTo(reflect.PtrTo(bigInt)):
		return decodeBigInt, nil
	case typ.AssignableTo(bigInt):
		return decodeBigIntNoPtr, nil
	case kind == reflect.Ptr:
		return makePtrDecoder(typ, tags)
	case reflect.PtrTo(typ).Implements(decoderInterface):
		return decodeDecoder, nil
	case isUint(kind):
		return decodeUint, nil
	case kind == reflect.Bool:
		return decodeBool, nil
	case kind == reflect.String:
		return decodeString, nil
	case kind == reflect.Slice || kind == reflect.Array:
		return makeListDecoder(typ, tags)
	case kind == reflect.Struct:
		return makeStructDecoder(typ)
	case kind == reflect.Interface:
		return decodeInterface, nil
	default:
		return nil, fmt.Errorf("rlp: type %v is not RLP-serializable", typ)
	}
}

func decodeRawValue(s *Stream, val reflect.Value) error {
	r, err := s.Raw()
	if err != nil {
		return err
	}
	val.SetBytes(r)
	return nil
}

func decodeUint(s *Stream, val reflect.Value) error {
	typ := val.Type()
	num, err := s.uint(typ.Bits())
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	val.SetUint(num)
	return nil
}

func decodeBool(s *Stream, val reflect.Value) error {
	b, err := s.Bool()
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	val.SetBool(b)
	return nil
}

func decodeString(s *Stream, val reflect.Value) error {
	b, err := s.Bytes()
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	val.SetString(string(b))
	return nil
}

func decodeBigIntNoPtr(s *Stream, val reflect.Value) error {
	return decodeBigInt(s, val.Addr())
}

func decodeBigInt(s *Stream, val reflect.Value) error {
	i := val.Interface().(*big.Int)
	if i == nil {
		i = new(big.Int)
		val.Set(reflect.ValueOf(i))
	}

	err := s.decodeBigInt(i)
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	return nil
}

func makeListDecoder(typ reflect.Type, tag rlpstruct.Tags) (decoder, error) {
	etype := typ.Elem()
	if etype.Kind() == reflect.Uint8 && !reflect.PtrTo(etype).Implements(decoderInterface) {
		if typ.Kind() == reflect.Array {
			return decodeByteArray, nil
		}
		return decodeByteSlice, nil
	}
	etypeinfo := theTC.infoWhileGenerating(etype, rlpstruct.Tags{})
	if etypeinfo.decoderErr != nil {
		return nil, etypeinfo.decoderErr
	}
	var dec decoder
	switch {
	case typ.Kind() == reflect.Array:
		dec = func(s *Stream, val reflect.Value) error {
			return decodeListArray(s, val, etypeinfo.decoder)
		}
	case tag.Tail:
		// A slice with "tail" tag can occur as the last field
		// of a struct and is supposed to swallow all remaining
		// list elements. The struct decoder already called s.List,
		// proceed directly to decoding the elements.
		dec = func(s *Stream, val reflect.Value) error {
			return decodeSliceElems(s, val, etypeinfo.decoder)
		}
	default:
		dec = func(s *Stream, val reflect.Value) error {
			return decodeListSlice(s, val, etypeinfo.decoder)
		}
	}
	return dec, nil
}

func decodeListSlice(s *Stream, val reflect.Value, elemdec decoder) error {
	size, err := s.List()
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	if size == 0 {
		val.Set(reflect.MakeSlice(val.Type(), 0, 0))
		return s.ListEnd()
	}
	if err := decodeSliceElems(s, val, elemdec); err != nil {
		return err
	}
	return s.ListEnd()
}

func decodeSliceElems(s *Stream, val reflect.Value, elemdec decoder) error {
	i := 0
	for ; ; i++ {
		// grow slice if necessary
		if i >= val.Cap() {
			newcap := val.Cap() + val.Cap()/2
			if newcap < 4 {
				newcap = 4
			}
			newv := reflect.MakeSlice(val.Type(), val.Len(), newcap)
			reflect.Copy(newv, val)
			val.Set(newv)
		}
		if i >= val.Len() {
			val.SetLen(i + 1)
		}
		// decode into element
		if err := elemdec(s, val.Index(i)); err == EOL {
			break
		} else if err != nil {
			return addErrorContext(err, fmt.Sprint("[", i, "]"))
		}
	}
	if i < val.Len() {
		val.SetLen(i)
	}
	return nil
}

func decodeListArray(s *Stream, val reflect.Value, elemdec decoder) error {
	if _, err := s.List(); err != nil {
		return wrapStreamError(err, val.Type())
	}
	vlen := val.Len()
	i := 0
	for ; i < vlen; i++ {
		if err := elemdec(s, val.Index(i)); err == EOL {
			break
		} else if err != nil {
			return addErrorContext(err, fmt.Sprint("[", i, "]"))
		}
	}
	if i < vlen {
		return &decodeError{msg: "input list has too few elements", typ: val.Type()}
	}
	return wrapStreamError(s.ListEnd(), val.Type())
}

func decodeByteSlice(s *Stream, val reflect.Value) error {
	b, err := s.Bytes()
	if err != nil {
		return wrapStreamError(err, val.Type())
	}
	val.SetBytes(b)
	return nil
}

func decodeByteArray(s *Stream, val reflect.Value) error {
	kind, size, err := s.Kind()
	if err != nil {
		return err
	}
	slice := byteArrayBytes(val, val.Len())
	switch kind {
	case Byte:
		if len(slice) == 0 {
			return &decodeError{msg: "input string too long", typ: val.Type()}
		} else if len(slice) > 1 {
			return &decodeError{msg: "input string too short", typ: val.Type()}
		}
		slice[0] = s.byteval
		s.kind = -1
	case String:
		if uint64(len(slice)) < size {
			return &decodeError{msg: "input string too long", typ: val.Type()}
		}
		if uint64(len(slice)) > size {
			return &decodeError{msg: "input string too short", typ: val.Type()}
		}
		if err := s.readFull(slice); err != nil {
			return err
		}
		// Reject cases where single byte encoding should have been used.
		if size == 1 && slice[0] < 128 {
			return wrapStreamError(ErrCanonSize, val.Type())
		}
	case List:
		return wrapStreamError(ErrExpectedString, val.Type())
	}
	return nil
}

func makeStructDecoder(typ reflect.Type) (decoder, error) {
	fields, err := structFields(typ)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if f.info.decoderErr != nil {
			return nil, structFieldError{typ, f.index, f.info.decoderErr}
		}
	}
	dec := func(s *Stream, val reflect.Value) (err error) {
		if _, err := s.List(); err != nil {
			return wrapStreamError(err, typ)
		}
		for i, f := range fields {
			err := f.info.decoder(s, val.Field(f.index))
			if err == EOL {
				if f.optional {
					// The field is optional, so reaching the end of the list before
					// reaching the last field is acceptable. All remaining undecoded
					// fields are zeroed.
					zeroFields(val, fields[i:])
					break
				}
				return &decodeError{msg: "too few elements", typ: typ}
			} else if err != nil {
				return addErrorContext(err, "."+typ.Field(f.index).Name)
			}
		}
		return wrapStreamError(s.ListEnd(), typ)
	}
	return dec, nil
}

func zeroFields(structval reflect.Value, fields []field) {
	for _, f := range fields {
		fv := structval.Field(f.index)
		fv.Set(reflect.Zero(fv.Type()))
	}
}

// makePtrDecoder creates a decoder that decodes into the pointer's element type.
func makePtrDecoder(typ reflect.Type, tag rlpstruct.Tags) (decoder, error) {
	etype := typ.Elem()
	etypeinfo := theTC.infoWhileGenerating(etype, rlpstruct.Tags{})
	switch {
	case etypeinfo.decoderErr != nil:
		return nil, etypeinfo.decoderErr
	case !tag.NilOK:
		return makeSimplePtrDecoder(etype, etypeinfo), nil
	default:
		return makeNilPtrDecoder(etype, etypeinfo, tag), nil
	}
}

func makeSimplePtrDecoder(etype reflect.Type, etypeinfo *typeinfo) decoder {
	return func(s *Stream, val reflect.Value) (err error) {
		newval := val
		if val.IsNil() {
			newval = reflect.New(etype)
		}
		if err = etypeinfo.decoder(s, newval.Elem()); err == nil {
			val.Set(newval)
		}
		return err
	}
}

// makeNilPtrDecoder creates a decoder that decodes empty values as nil. Non-empty
// values are decoded into a value of the element type, just like makePtrDecoder does.
//
// This decoder is used for pointer-typed struct fields with struct tag "nil".
func makeNilPtrDecoder(etype reflect.Type, etypeinfo *typeinfo, ts rlpstruct.Tags) decoder {
	typ := reflect.PtrTo(etype)
	nilPtr := reflect.Zero(typ)

	// Determine the value kind that results in nil pointer.
	nilKind := typeNilKind(etype, ts)

	return func(s *Stream, val reflect.Value) (err error) {
		kind, size, err := s.Kind()
		if err != nil {
			val.Set(nilPtr)
			return wrapStreamError(err, typ)
		}
		// Handle empty values as a nil pointer.
		if kind != Byte && size == 0 {
			if kind != nilKind {
				return &decodeError{
					msg: fmt.Sprintf("wrong kind of empty value (got %v, want %v)", kind, nilKind),
					typ: typ,
				}
			}
			// rearm s.Kind. This is important because the input
			// position must advance to the next value even though
			// we don't read anything.
			s.kind = -1
			val.Set(nilPtr)
			return nil
		}
		newval := val
		if val.IsNil() {
			newval = reflect.New(etype)
		}
		if err = etypeinfo.decoder(s, newval.Elem()); err == nil {
			val.Set(newval)
		}
		return err
	}
}

var ifsliceType = reflect.TypeOf([]interface{}{})

func decodeInterface(s *Stream, val reflect.Value) error {
	if val.Type().NumMethod() != 0 {
		return fmt.Errorf("rlp: type %v is not RLP-serializable", val.Type())
	}
	kind, _, err := s.Kind()
	if err != nil {
		return err
	}
	if kind == List {
		slice := reflect.New(ifsliceType).Elem()
		if err := decodeListSlice(s, slice, decodeInterface); err != nil {
			return err
		}
		val.Set(slice)
	} else {
		b, err := s.Bytes()
		if err != nil {
			return err
		}
		val.Set(reflect.ValueOf(b))
	}
	return nil
}

func decodeDecoder(s *Stream, val reflect.Value) error {
	return val.Addr().Interface().(Decoder).DecodeRLP(s)
}

// Kind represents the kind of value contained in an RLP stream.
type Kind int8

const (
	Byte Kind = iota
	String
	List
)

func (k Kind) String() string {
	switch k {
	case Byte:
		return "Byte"
	case String:
		return "String"
	case List:
		return "List"
	default:
		return fmt.Sprintf("Unknown(%d)", k)
	}
}

// ByteReader must be implemented by any input reader for a Stream. It
// is implemented by e.g. bufio.Reader and bytes.Reader.
type ByteReader interface {
	io.Reader
	io.ByteReader
}

//流可用于输入流的零碎解码。这个
//如果输入很大，或者如果解码规则
//类型取决于输入结构。流不保留
//内部缓冲区。解码值后，输入读取器将为
//位于下一个值的类型信息之前。
//
//解码列表和输入位置到达已声明的
//列表的长度，所有操作都将返回错误EOL。
//必须使用ListEnd确认列表的结尾以继续
//阅读封闭列表。
//
//流并非安全。
type Stream struct {
	r ByteReader

	remaining uint64   // number of bytes remaining to be read from r
	size      uint64   // size of value ahead
	kinderr   error    // error from last readKind
	stack     []uint64 // list sizes
	uintbuf   [32]byte // auxiliary buffer for integer decoding
	kind      Kind     // kind of value ahead
	byteval   byte     // value of single byte in type tag
	limited   bool     // true if input limit is in effect
}

// newstream创建了r的新解码流读数。
//
//如果r实现Bytereader接口，流将
//不要引入任何缓冲。
//
//对于非级别值，流返回errelemtoolarge
//对于不适合封闭列表的值。
//
//流支持可选的输入限制。如果设置了限制，
//将检查剩余的任何高级值的大小
//输入长度。遇到超过值的流操作
//剩余的输入长度将返回frervaluetoolarge。极限
//可以通过传递输入限制的非零值来设置。
//
//如果r是bytes.reader或strings.reader，则将输入限制设置为
// R的基础数据长度除非显式限制为
// 假如。
func NewStream(r io.Reader, inputLimit uint64) *Stream {
	s := new(Stream)
	s.Reset(r, inputLimit)
	return s
}

//NewListStream创建了一个假装位置的新流
//在给定长度的编码列表中。
func NewListStream(r io.Reader, len uint64) *Stream {
	s := new(Stream)
	s.Reset(r, len)
	s.kind = List
	s.size = len
	return s
}

//字节读取RLP字符串，并将其内容返回为字节切片。
//如果输入不包含RLP字符串，则返回
//错误将是errexpectedString。
func (s *Stream) Bytes() ([]byte, error) {
	kind, size, err := s.Kind()
	if err != nil {
		return nil, err
	}
	switch kind {
	case Byte:
		s.kind = -1 // rearm Kind
		return []byte{s.byteval}, nil
	case String:
		b := make([]byte, size)
		if err = s.readFull(b); err != nil {
			return nil, err
		}
		if size == 1 && b[0] < 128 {
			return nil, ErrCanonSize
		}
		return b, nil
	default:
		return nil, ErrExpectedString
	}
}

// ReadBytes decodes the next RLP value and stores the result in b.
// The value size must match len(b) exactly.
func (s *Stream) ReadBytes(b []byte) error {
	kind, size, err := s.Kind()
	if err != nil {
		return err
	}
	switch kind {
	case Byte:
		if len(b) != 1 {
			return fmt.Errorf("input value has wrong size 1, want %d", len(b))
		}
		b[0] = s.byteval
		s.kind = -1 // rearm Kind
		return nil
	case String:
		if uint64(len(b)) != size {
			return fmt.Errorf("input value has wrong size %d, want %d", size, len(b))
		}
		if err = s.readFull(b); err != nil {
			return err
		}
		if size == 1 && b[0] < 128 {
			return ErrCanonSize
		}
		return nil
	default:
		return ErrExpectedString
	}
}

// Raw reads a raw encoded value including RLP type information.
func (s *Stream) Raw() ([]byte, error) {
	kind, size, err := s.Kind()
	if err != nil {
		return nil, err
	}
	if kind == Byte {
		s.kind = -1 // rearm Kind
		return []byte{s.byteval}, nil
	}
	// The original header has already been read and is no longer
	// available. Read content and put a new header in front of it.
	start := headsize(size)
	buf := make([]byte, uint64(start)+size)
	if err := s.readFull(buf[start:]); err != nil {
		return nil, err
	}
	if kind == String {
		puthead(buf, 0x80, 0xB7, size)
	} else {
		puthead(buf, 0xC0, 0xF7, size)
	}
	return buf, nil
}

// Uint reads an RLP string of up to 8 bytes and returns its contents
// as an unsigned integer. If the input does not contain an RLP string, the
// returned error will be ErrExpectedString.
//
// Deprecated: use s.Uint64 instead.
func (s *Stream) Uint() (uint64, error) {
	return s.uint(64)
}

func (s *Stream) Uint64() (uint64, error) {
	return s.uint(64)
}

func (s *Stream) Uint32() (uint32, error) {
	i, err := s.uint(32)
	return uint32(i), err
}

func (s *Stream) Uint16() (uint16, error) {
	i, err := s.uint(16)
	return uint16(i), err
}

func (s *Stream) Uint8() (uint8, error) {
	i, err := s.uint(8)
	return uint8(i), err
}

func (s *Stream) uint(maxbits int) (uint64, error) {
	kind, size, err := s.Kind()
	if err != nil {
		return 0, err
	}
	switch kind {
	case Byte:
		if s.byteval == 0 {
			return 0, ErrCanonInt
		}
		s.kind = -1 // rearm Kind
		return uint64(s.byteval), nil
	case String:
		if size > uint64(maxbits/8) {
			return 0, errUintOverflow
		}
		v, err := s.readUint(byte(size))
		switch {
		case err == ErrCanonSize:
			// Adjust error because we're not reading a size right now.
			return 0, ErrCanonInt
		case err != nil:
			return 0, err
		case size > 0 && v < 128:
			return 0, ErrCanonSize
		default:
			return v, nil
		}
	default:
		return 0, ErrExpectedString
	}
}

// Bool reads an RLP string of up to 1 byte and returns its contents
// as a boolean. If the input does not contain an RLP string, the
// returned error will be ErrExpectedString.
func (s *Stream) Bool() (bool, error) {
	num, err := s.uint(8)
	if err != nil {
		return false, err
	}
	switch num {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("rlp: invalid boolean value: %d", num)
	}
}

// List starts decoding an RLP list. If the input does not contain a
// list, the returned error will be ErrExpectedList. When the list's
// end has been reached, any Stream operation will return EOL.
func (s *Stream) List() (size uint64, err error) {
	kind, size, err := s.Kind()
	if err != nil {
		return 0, err
	}
	if kind != List {
		return 0, ErrExpectedList
	}

	// Remove size of inner list from outer list before pushing the new size
	// onto the stack. This ensures that the remaining outer list size will
	// be correct after the matching call to ListEnd.
	if inList, limit := s.listLimit(); inList {
		s.stack[len(s.stack)-1] = limit - size
	}
	s.stack = append(s.stack, size)
	s.kind = -1
	s.size = 0
	return size, nil
}

// ListEnd returns to the enclosing list.
// The input reader must be positioned at the end of a list.
func (s *Stream) ListEnd() error {
	// Ensure that no more data is remaining in the current list.
	if inList, listLimit := s.listLimit(); !inList {
		return errNotInList
	} else if listLimit > 0 {
		return errNotAtEOL
	}
	s.stack = s.stack[:len(s.stack)-1] // pop
	s.kind = -1
	s.size = 0
	return nil
}

// MoreDataInList reports whether the current list context contains
// more data to be read.
func (s *Stream) MoreDataInList() bool {
	_, listLimit := s.listLimit()
	return listLimit > 0
}

// BigInt decodes an arbitrary-size integer value.
func (s *Stream) BigInt() (*big.Int, error) {
	i := new(big.Int)
	if err := s.decodeBigInt(i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Stream) decodeBigInt(dst *big.Int) error {
	var buffer []byte
	kind, size, err := s.Kind()
	switch {
	case err != nil:
		return err
	case kind == List:
		return ErrExpectedString
	case kind == Byte:
		buffer = s.uintbuf[:1]
		buffer[0] = s.byteval
		s.kind = -1 // re-arm Kind
	case size == 0:
		// Avoid zero-length read.
		s.kind = -1
	case size <= uint64(len(s.uintbuf)):
		// For integers smaller than s.uintbuf, allocating a buffer
		// can be avoided.
		buffer = s.uintbuf[:size]
		if err := s.readFull(buffer); err != nil {
			return err
		}
		// Reject inputs where single byte encoding should have been used.
		if size == 1 && buffer[0] < 128 {
			return ErrCanonSize
		}
	default:
		// For large integers, a temporary buffer is needed.
		buffer = make([]byte, size)
		if err := s.readFull(buffer); err != nil {
			return err
		}
	}

	// Reject leading zero bytes.
	if len(buffer) > 0 && buffer[0] == 0 {
		return ErrCanonInt
	}
	// Set the integer bytes.
	dst.SetBytes(buffer)
	return nil
}

// Decode decodes a value and stores the result in the value pointed
// to by val. Please see the documentation for the Decode function
// to learn about the decoding rules.
func (s *Stream) Decode(val interface{}) error {
	if val == nil {
		return errDecodeIntoNil
	}
	rval := reflect.ValueOf(val)
	rtyp := rval.Type()
	if rtyp.Kind() != reflect.Ptr {
		return errNoPointer
	}
	if rval.IsNil() {
		return errDecodeIntoNil
	}
	decoder, err := cachedDecoder(rtyp.Elem())
	if err != nil {
		return err
	}

	err = decoder(s, rval.Elem())
	if decErr, ok := err.(*decodeError); ok && len(decErr.ctx) > 0 {
		// Add decode target type to error so context has more meaning.
		decErr.ctx = append(decErr.ctx, fmt.Sprint("(", rtyp.Elem(), ")"))
	}
	return err
}

// Reset discards any information about the current decoding context
// and starts reading from r. This method is meant to facilitate reuse
// of a preallocated Stream across many decoding operations.
//
// If r does not also implement ByteReader, Stream will do its own
// buffering.
func (s *Stream) Reset(r io.Reader, inputLimit uint64) {
	if inputLimit > 0 {
		s.remaining = inputLimit
		s.limited = true
	} else {
		// Attempt to automatically discover
		// the limit when reading from a byte slice.
		switch br := r.(type) {
		case *bytes.Reader:
			s.remaining = uint64(br.Len())
			s.limited = true
		case *bytes.Buffer:
			s.remaining = uint64(br.Len())
			s.limited = true
		case *strings.Reader:
			s.remaining = uint64(br.Len())
			s.limited = true
		default:
			s.limited = false
		}
	}
	// Wrap r with a buffer if it doesn't have one.
	bufr, ok := r.(ByteReader)
	if !ok {
		bufr = bufio.NewReader(r)
	}
	s.r = bufr
	// Reset the decoding context.
	s.stack = s.stack[:0]
	s.size = 0
	s.kind = -1
	s.kinderr = nil
	s.byteval = 0
	s.uintbuf = [32]byte{}
}

// Kind returns the kind and size of the next value in the
// input stream.
//
// The returned size is the number of bytes that make up the value.
// For kind == Byte, the size is zero because the value is
// contained in the type tag.
//
// The first call to Kind will read size information from the input
// reader and leave it positioned at the start of the actual bytes of
// the value. Subsequent calls to Kind (until the value is decoded)
// will not advance the input reader and return cached information.
func (s *Stream) Kind() (kind Kind, size uint64, err error) {
	if s.kind >= 0 {
		return s.kind, s.size, s.kinderr
	}

	// Check for end of list. This needs to be done here because readKind
	// checks against the list size, and would return the wrong error.
	inList, listLimit := s.listLimit()
	if inList && listLimit == 0 {
		return 0, 0, EOL
	}
	// Read the actual size tag.
	s.kind, s.size, s.kinderr = s.readKind()
	if s.kinderr == nil {
		// Check the data size of the value ahead against input limits. This
		// is done here because many decoders require allocating an input
		// buffer matching the value size. Checking it here protects those
		// decoders from inputs declaring very large value size.
		if inList && s.size > listLimit {
			s.kinderr = ErrElemTooLarge
		} else if s.limited && s.size > s.remaining {
			s.kinderr = ErrValueTooLarge
		}
	}
	return s.kind, s.size, s.kinderr
}

func (s *Stream) readKind() (kind Kind, size uint64, err error) {
	b, err := s.readByte()
	if err != nil {
		if len(s.stack) == 0 {
			// At toplevel, Adjust the error to actual EOF. io.EOF is
			// used by callers to determine when to stop decoding.
			switch err {
			case io.ErrUnexpectedEOF:
				err = io.EOF
			case ErrValueTooLarge:
				err = io.EOF
			}
		}
		return 0, 0, err
	}
	s.byteval = 0
	switch {
	case b < 0x80:
		// For a single byte whose value is in the [0x00, 0x7F] range, that byte
		// is its own RLP encoding.
		s.byteval = b
		return Byte, 0, nil
	case b < 0xB8:
		// Otherwise, if a string is 0-55 bytes long, the RLP encoding consists
		// of a single byte with value 0x80 plus the length of the string
		// followed by the string. The range of the first byte is thus [0x80, 0xB7].
		return String, uint64(b - 0x80), nil
	case b < 0xC0:
		// If a string is more than 55 bytes long, the RLP encoding consists of a
		// single byte with value 0xB7 plus the length of the length of the
		// string in binary form, followed by the length of the string, followed
		// by the string. For example, a length-1024 string would be encoded as
		// 0xB90400 followed by the string. The range of the first byte is thus
		// [0xB8, 0xBF].
		size, err = s.readUint(b - 0xB7)
		if err == nil && size < 56 {
			err = ErrCanonSize
		}
		return String, size, err
	case b < 0xF8:
		// If the total payload of a list (i.e. the combined length of all its
		// items) is 0-55 bytes long, the RLP encoding consists of a single byte
		// with value 0xC0 plus the length of the list followed by the
		// concatenation of the RLP encodings of the items. The range of the
		// first byte is thus [0xC0, 0xF7].
		return List, uint64(b - 0xC0), nil
	default:
		// If the total payload of a list is more than 55 bytes long, the RLP
		// encoding consists of a single byte with value 0xF7 plus the length of
		// the length of the payload in binary form, followed by the length of
		// the payload, followed by the concatenation of the RLP encodings of
		// the items. The range of the first byte is thus [0xF8, 0xFF].
		size, err = s.readUint(b - 0xF7)
		if err == nil && size < 56 {
			err = ErrCanonSize
		}
		return List, size, err
	}
}

func (s *Stream) readUint(size byte) (uint64, error) {
	switch size {
	case 0:
		s.kind = -1 // rearm Kind
		return 0, nil
	case 1:
		b, err := s.readByte()
		return uint64(b), err
	default:
		buffer := s.uintbuf[:8]
		for i := range buffer {
			buffer[i] = 0
		}
		start := int(8 - size)
		if err := s.readFull(buffer[start:]); err != nil {
			return 0, err
		}
		if buffer[start] == 0 {
			// Note: readUint is also used to decode integer values.
			// The error needs to be adjusted to become ErrCanonInt in this case.
			return 0, ErrCanonSize
		}
		return binary.BigEndian.Uint64(buffer[:]), nil
	}
}

// readFull reads into buf from the underlying stream.
func (s *Stream) readFull(buf []byte) (err error) {
	if err := s.willRead(uint64(len(buf))); err != nil {
		return err
	}
	var nn, n int
	for n < len(buf) && err == nil {
		nn, err = s.r.Read(buf[n:])
		n += nn
	}
	if err == io.EOF {
		if n < len(buf) {
			err = io.ErrUnexpectedEOF
		} else {
			// Readers are allowed to give EOF even though the read succeeded.
			// In such cases, we discard the EOF, like io.ReadFull() does.
			err = nil
		}
	}
	return err
}

// readByte reads a single byte from the underlying stream.
func (s *Stream) readByte() (byte, error) {
	if err := s.willRead(1); err != nil {
		return 0, err
	}
	b, err := s.r.ReadByte()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return b, err
}

// willRead is called before any read from the underlying stream. It checks
// n against size limits, and updates the limits if n doesn't overflow them.
func (s *Stream) willRead(n uint64) error {
	s.kind = -1 // rearm Kind

	if inList, limit := s.listLimit(); inList {
		if n > limit {
			return ErrElemTooLarge
		}
		s.stack[len(s.stack)-1] = limit - n
	}
	if s.limited {
		if n > s.remaining {
			return ErrValueTooLarge
		}
		s.remaining -= n
	}
	return nil
}

// listLimit returns the amount of data remaining in the innermost list.
func (s *Stream) listLimit() (inList bool, limit uint64) {
	if len(s.stack) == 0 {
		return false, 0
	}
	return true, s.stack[len(s.stack)-1]
}
