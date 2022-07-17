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

//软件包ETHDB定义以太坊数据存储的接口。
package ethdb

import "io"

// KeyValueReader包装了备用数据存储的HAS和获取方法。
type KeyValueReader interface {
	// 如果键值数据存储中存在键，则可以进行检索。
	Has(key []byte) (bool, error)

	// 如果给定的键在键值数据存储中，请获取检索。
	Get(key []byte) ([]byte, error)
}

// KeyValueWriter包装了备用数据存储的PUT方法。
type KeyValueWriter interface {
	// Put inserts the given value into the key-value data store.
	Put(key []byte, value []byte) error

	// Delete removes the key from the key-value data store.
	Delete(key []byte) error
}

// KeyValueStater wraps the Stat method of a backing data store.
type KeyValueStater interface {
	// Stat returns a particular internal stat of the database.
	Stat(property string) (string, error)
}

// Compacter包装了备用数据存储的紧凑方法。
type Compacter interface {
	//紧凑型给定关键范围的基础数据存储。在本质上，
//删除和覆盖版本被丢弃，数据重新排列为
//降低访问它们所需的运营成本。
//
//零启动被视为数据存储中所有键之前的钥匙；零限制
//被视为数据存储中所有键后的钥匙。如果两者都是零，那
//将紧凑整个数据存储。
	Compact(start []byte, limit []byte) error
}

// KeyValueStore contains all the methods required to allow handling different
// key-value data stores backing the high level database.
type KeyValueStore interface {
	KeyValueReader
	KeyValueWriter
	KeyValueStater
	Batcher
	Iteratee
	Compacter
	Snapshotter
	io.Closer
}

// AncientReaderOp contains the methods required to read from immutable ancient data.
type AncientReaderOp interface {
	// HasAncient returns an indicator whether the specified data exists in the
	// ancient store.
	HasAncient(kind string, number uint64) (bool, error)

	// Ancient retrieves an ancient binary blob from the append-only immutable files.
	Ancient(kind string, number uint64) ([]byte, error)

	// AncientRange retrieves multiple items in sequence, starting from the index 'start'.
	// It will return
	//  - at most 'count' items,
	//  - at least 1 item (even if exceeding the maxBytes), but will otherwise
	//   return as many items as fit into maxBytes.
	AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error)

	// Ancients returns the ancient item numbers in the ancient store.
	Ancients() (uint64, error)

	// Tail returns the number of first stored item in the freezer.
	// This number can also be interpreted as the total deleted item numbers.
	Tail() (uint64, error)

	// AncientSize returns the ancient size of the specified category.
	AncientSize(kind string) (uint64, error)
}

// AncientReader is the extended ancient reader interface including 'batched' or 'atomic' reading.
type AncientReader interface {
	AncientReaderOp

	// ReadAncients runs the given read operation while ensuring that no writes take place
	// on the underlying freezer.
	ReadAncients(fn func(AncientReaderOp) error) (err error)
}

// AncientWriter contains the methods required to write to immutable ancient data.
type AncientWriter interface {
	// ModifyAncients runs a write operation on the ancient store.
	// If the function returns an error, any changes to the underlying store are reverted.
	// The integer return value is the total size of the written data.
	ModifyAncients(func(AncientWriteOp) error) (int64, error)

	// TruncateHead discards all but the first n ancient data from the ancient store.
	// After the truncation, the latest item can be accessed it item_n-1(start from 0).
	TruncateHead(n uint64) error

	// TruncateTail discards the first n ancient data from the ancient store. The already
	// deleted items are ignored. After the truncation, the earliest item can be accessed
	// is item_n(start from 0). The deleted items may not be removed from the ancient store
	// immediately, but only when the accumulated deleted data reach the threshold then
	// will be removed all together.
	TruncateTail(n uint64) error

	// Sync flushes all in-memory ancient store data to disk.
	Sync() error

	// MigrateTable processes and migrates entries of a given table to a new format.
	// The second argument is a function that takes a raw entry and returns it
	// in the newest format.
	MigrateTable(string, func([]byte) ([]byte, error)) error
}

// AncientWriteOp is given to the function argument of ModifyAncients.
type AncientWriteOp interface {
	// Append adds an RLP-encoded item.
	Append(kind string, number uint64, item interface{}) error

	// AppendRaw adds an item without RLP-encoding it.
	AppendRaw(kind string, number uint64, item []byte) error
}

// AncientStater wraps the Stat method of a backing data store.
type AncientStater interface {
	// AncientDatadir returns the root directory path of the ancient store.
	AncientDatadir() (string, error)
}

//读者包含读取键值数据所需的方法以及
//不变的古代数据。
type Reader interface {
	KeyValueReader
	AncientReader
}

// Writer包含将数据写入键值所需的方法以及
//不变的古代数据。
type Writer interface {
	KeyValueWriter
	AncientWriter
}


// Stater包含从键值和
//不变的古代数据。
type Stater interface {
	KeyValueStater
	AncientStater
}

//古店包含允许处理不同所需的所有方法
//古代数据存储备份不变的链数据存储。
type AncientStore interface {
	AncientReader
	AncientWriter
	AncientStater
	io.Closer
}

//数据库包含高级数据库所需的所有方法
//仅访问键值数据存储，还可以访问链条冰柜。
type Database interface {
	Reader
	Writer
	Batcher
	Iteratee
	Stater
	Compacter
	Snapshotter
	io.Closer
}
