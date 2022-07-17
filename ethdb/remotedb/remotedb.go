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

//包装远程BEXPENDB基于远程Geth实现键值数据库层
//节点。在引擎盖下，它利用“ debug_dbget”方法实现
//仅读取数据库。
//在此数据库中确实没有保证，因为本地Geth没有
//独家访问，但可以用于远程节点的基本诊断。
package remotedb

import (
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rpc"
)

// Database is a key-value lookup for a remote database via debug_dbGet.
type Database struct {
	remote *rpc.Client
}

func (db *Database) Has(key []byte) (bool, error) {
	if _, err := db.Get(key); err != nil {
		return false, nil
	}
	return true, nil
}

func (db *Database) Get(key []byte) ([]byte, error) {
	var resp hexutil.Bytes
	err := db.remote.Call(&resp, "debug_dbGet", hexutil.Bytes(key))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (db *Database) HasAncient(kind string, number uint64) (bool, error) {
	if _, err := db.Ancient(kind, number); err != nil {
		return false, nil
	}
	return true, nil
}

func (db *Database) Ancient(kind string, number uint64) ([]byte, error) {
	var resp hexutil.Bytes
	err := db.remote.Call(&resp, "debug_dbAncient", kind, number)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (db *Database) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	panic("not supported")
}

func (db *Database) Ancients() (uint64, error) {
	var resp uint64
	err := db.remote.Call(&resp, "debug_dbAncients")
	return resp, err
}

func (db *Database) Tail() (uint64, error) {
	panic("not supported")
}

func (db *Database) AncientSize(kind string) (uint64, error) {
	panic("not supported")
}

func (db *Database) ReadAncients(fn func(op ethdb.AncientReaderOp) error) (err error) {
	return fn(db)
}

func (db *Database) Put(key []byte, value []byte) error {
	panic("not supported")
}

func (db *Database) Delete(key []byte) error {
	panic("not supported")
}

func (db *Database) ModifyAncients(f func(ethdb.AncientWriteOp) error) (int64, error) {
	panic("not supported")
}

func (db *Database) TruncateHead(n uint64) error {
	panic("not supported")
}

func (db *Database) TruncateTail(n uint64) error {
	panic("not supported")
}

func (db *Database) Sync() error {
	return nil
}

func (db *Database) MigrateTable(s string, f func([]byte) ([]byte, error)) error {
	panic("not supported")
}

func (db *Database) NewBatch() ethdb.Batch {
	panic("not supported")
}

func (db *Database) NewBatchWithSize(size int) ethdb.Batch {
	panic("not supported")
}

func (db *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	panic("not supported")
}

func (db *Database) Stat(property string) (string, error) {
	panic("not supported")
}

func (db *Database) AncientDatadir() (string, error) {
	panic("not supported")
}

func (db *Database) Compact(start []byte, limit []byte) error {
	return nil
}

func (db *Database) NewSnapshot() (ethdb.Snapshot, error) {
	panic("not supported")
}

func (db *Database) Close() error {
	db.remote.Close()
	return nil
}

func dialRPC(endpoint string) (*rpc.Client, error) {
	if endpoint == "" {
		return nil, errors.New("endpoint must be specified")
	}
	if strings.HasPrefix(endpoint, "rpc:") || strings.HasPrefix(endpoint, "ipc:") {
		// Backwards compatibility with geth < 1.5 which required
		// these prefixes.
		endpoint = endpoint[4:]
	}
	return rpc.Dial(endpoint)
}

func New(endpoint string) (ethdb.Database, error) {
	client, err := dialRPC(endpoint)
	if err != nil {
		return nil, err
	}
	return &Database{
		remote: client,
	}, nil
}
