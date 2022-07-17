//版权所有2018年作者
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

package ethdb

// ifealbatchsize定义数据批的大小应理想地添加一个
// 写。
const IdealBatchSize = 100 * 1024

// Batch是一个仅写入数据库，可更改其主机数据库
//当写入时。批次不能同时使用。
type Batch interface {
	KeyValueWriter

	// 重视检索编写的排队数据的数量。
	ValueSize() int

	//将汇总到磁盘上的所有累积数据。
	Write() error

	// 重置重置批次以重复使用。
	Reset()

	// 重播批处理内容。
	Replay(w KeyValueWriter) error
}

// Batcher包装了备用数据存储的NewBatch方法。
type Batcher interface {
	// NewBatch创建一个仅写入数据库，该数据库缓冲到其主机DB
    //直到最终写入为止。
	NewBatch() Batch

	// NewBatchWithSize创建一个只有写入的数据库批处理，并具有预先分配的缓冲区。
	NewBatchWithSize(size int) Batch
}

// hookedbatch包裹了一个任意批次，每个操作都可以挂入
//从黑框代码监视。
type HookedBatch struct {
	Batch

	OnPut    func(key []byte, value []byte) // Callback if a key is inserted
	OnDelete func(key []byte)               // Callback if a key is deleted
}

// 将给定值插入到键值数据存储中。
func (b HookedBatch) Put(key []byte, value []byte) error {
	if b.OnPut != nil {
		b.OnPut(key, value)
	}
	return b.Batch.Put(key, value)
}

// 删除从密钥值数据存储中删除密钥。
func (b HookedBatch) Delete(key []byte) error {
	if b.OnDelete != nil {
		b.OnDelete(key)
	}
	return b.Batch.Delete(key)
}
