//版权所有2014年作者
//此文件是Go-Ethereum的一部分。
//
// Go-Ethereum是免费软件：您可以重新分配它和/或修改
//根据GNU通用公共许可证的条款发布
//免费软件基金会（许可证的3版本）或
//（根据您的选择）任何以后的版本。
//
// go-ethereum的分发是希望它有用的
//但没有任何保修；甚至没有暗示的保证
//适合或适合特定目的的健身。看到
// GNU通用公共许可证以获取更多详细信息。
//
//您应该收到GNU通用公共许可证的副本
//与Go-Ethereum一起。如果不是，请参见<http://www.gnu.org/licenses/>。

//软件包utils包含用于Go-Ethereum命令的内部辅助功能。
package utils

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfave/cli/v2"
)

const (
	importBatchSize = 2500
)

// FATALF将消息格式化为标准错误并退出程序。
//如果标准错误，也将消息打印到标准输出
//将其重定向到另一个文件。
func Fatalf(format string, args ...interface{}) {
	w := io.MultiWriter(os.Stdout, os.Stderr)
	if runtime.GOOS == "windows" {
		// 下面的同一文件检查在Windows上不起作用。 
        // STDOUT不太可能重定向，因此只需在此处打印即可。
		w = os.Stdout
	} else {
		outf, _ := os.Stdout.Stat()
		errf, _ := os.Stderr.Stat()
		if outf != nil && errf != nil && os.SameFile(outf, errf) {
			w = os.Stderr
		}
	}
	fmt.Fprintf(w, "Fatal: "+format+"\n", args...)
	os.Exit(1)
}

//TODO: 开启节点
func StartNode(ctx *cli.Context, stack *node.Node, isConsole bool) {
	// 启动节点
	if err := stack.Start(); err != nil {
		Fatalf("Error starting protocol stack: %v", err)
	}
	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigc)

		minFreeDiskSpace := 2 * ethconfig.Defaults.TrieDirtyCache // Default 2 * 256Mb
		if ctx.IsSet(MinFreeDiskSpaceFlag.Name) {
			minFreeDiskSpace = ctx.Int(MinFreeDiskSpaceFlag.Name)
		} else if ctx.IsSet(CacheFlag.Name) || ctx.IsSet(CacheGCFlag.Name) {
			minFreeDiskSpace = 2 * ctx.Int(CacheFlag.Name) * ctx.Int(CacheGCFlag.Name) / 100
		}
		if minFreeDiskSpace > 0 {
			go monitorFreeDiskSpace(sigc, stack.InstanceDir(), uint64(minFreeDiskSpace)*1024*1024)
		}

		shutdown := func() {
			log.Info("Got interrupt, shutting down...")
			go stack.Close()
			for i := 10; i > 0; i-- {
				<-sigc
				if i > 1 {
					log.Warn("Already shutting down, interrupt more to panic.", "times", i-1)
				}
			}
			debug.Exit() // ensure trace and CPU profile data is flushed.
			debug.LoudPanic("boom")
		}

		if isConsole {
			// In JS console mode, SIGINT is ignored because it's handled by the console.
			// However, SIGTERM still shuts down the node.
			for {
				sig := <-sigc
				if sig == syscall.SIGTERM {
					shutdown()
					return
				}
			}
		} else {
			<-sigc
			shutdown()
		}
	}()
}

// TODO: 监控免费磁盘空间
func monitorFreeDiskSpace(sigc chan os.Signal, path string, freeDiskSpaceCritical uint64) {
	for {
		freeSpace, err := getFreeDiskSpace(path)
		if err != nil {
			log.Warn("Failed to get free disk space", "path", path, "err", err)
			break
		}
		if freeSpace < freeDiskSpaceCritical {
			log.Error("Low disk space. Gracefully shutting down Geth to prevent database corruption.", "available", common.StorageSize(freeSpace))
			sigc <- syscall.SIGTERM
			break
		} else if freeSpace < 2*freeDiskSpaceCritical {
			log.Warn("Disk space is running low. Geth will shutdown if disk space runs below critical level.", "available", common.StorageSize(freeSpace), "critical_level", common.StorageSize(freeDiskSpaceCritical))
		}
		time.Sleep(30 * time.Second)
	}
}

func ImportChain(chain *core.BlockChain, fn string) error {
	// Watch for Ctrl-C while the import is running.
	// If a signal is received, the import will stop at the next batch.
	interrupt := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	defer close(interrupt)
	go func() {
		if _, ok := <-interrupt; ok {
			log.Info("Interrupted during import, stopping at next batch")
		}
		close(stop)
	}()
	checkInterrupt := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}

	log.Info("Importing blockchain", "file", fn)

	// Open the file handle and potentially unwrap the gzip stream
	fh, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer fh.Close()

	var reader io.Reader = fh
	if strings.HasSuffix(fn, ".gz") {
		if reader, err = gzip.NewReader(reader); err != nil {
			return err
		}
	}
	stream := rlp.NewStream(reader, 0)

	// Run actual the import.
	blocks := make(types.Blocks, importBatchSize)
	n := 0
	for batch := 0; ; batch++ {
		// Load a batch of RLP blocks.
		if checkInterrupt() {
			return fmt.Errorf("interrupted")
		}
		i := 0
		for ; i < importBatchSize; i++ {
			var b types.Block
			if err := stream.Decode(&b); err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("at block %d: %v", n, err)
			}
			// don't import first block
			if b.NumberU64() == 0 {
				i--
				continue
			}
			blocks[i] = &b
			n++
		}
		if i == 0 {
			break
		}
		// Import the batch.
		if checkInterrupt() {
			return fmt.Errorf("interrupted")
		}
		missing := missingBlocks(chain, blocks[:i])
		if len(missing) == 0 {
			log.Info("Skipping batch as all blocks present", "batch", batch, "first", blocks[0].Hash(), "last", blocks[i-1].Hash())
			continue
		}
		if _, err := chain.InsertChain(missing); err != nil {
			return fmt.Errorf("invalid block %d: %v", n, err)
		}
	}
	return nil
}

func missingBlocks(chain *core.BlockChain, blocks []*types.Block) []*types.Block {
	head := chain.CurrentBlock()
	for i, block := range blocks {
		// If we're behind the chain head, only check block, state is available at head
		if head.NumberU64() > block.NumberU64() {
			if !chain.HasBlock(block.Hash(), block.NumberU64()) {
				return blocks[i:]
			}
			continue
		}
		// If we're above the chain head, state availability is a must
		if !chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			return blocks[i:]
		}
	}
	return nil
}

// 导出链将区块链导出到指定的文件中，截断所有数据
// 文件中已经存在。
func ExportChain(blockchain *core.BlockChain, fn string) error {
	log.Info("Exporting blockchain", "file", fn)

	// Open the file handle and potentially wrap with a gzip stream
	fh, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer fh.Close()

	var writer io.Writer = fh
	if strings.HasSuffix(fn, ".gz") {
		writer = gzip.NewWriter(writer)
		defer writer.(*gzip.Writer).Close()
	}
	// Iterate over the blocks and export them
	if err := blockchain.Export(writer); err != nil {
		return err
	}
	log.Info("Exported blockchain", "file", fn)

	return nil
}

// 导出Appendchain将区块链导出到指定文件中，附加到
// 文件如果数据已经存在。
func ExportAppendChain(blockchain *core.BlockChain, fn string, first uint64, last uint64) error {
	log.Info("Exporting blockchain", "file", fn)

	// Open the file handle and potentially wrap with a gzip stream
	fh, err := os.OpenFile(fn, os.O_CREATE|os.O_APPEND|os.O_WRONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer fh.Close()

	var writer io.Writer = fh
	if strings.HasSuffix(fn, ".gz") {
		writer = gzip.NewWriter(writer)
		defer writer.(*gzip.Writer).Close()
	}
	// Iterate over the blocks and export them
	if err := blockchain.ExportN(writer, first, last); err != nil {
		return err
	}
	log.Info("Exported blockchain to", "file", fn)
	return nil
}

// ImportPreimages将一批导出的哈希预映射导入到数据库中。
//这是未来不推荐的功能的一部分。
func ImportPreimages(db ethdb.Database, fn string) error {
	log.Info("Importing preimages", "file", fn)

	// Open the file handle and potentially unwrap the gzip stream
	fh, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer fh.Close()

	var reader io.Reader = bufio.NewReader(fh)
	if strings.HasSuffix(fn, ".gz") {
		if reader, err = gzip.NewReader(reader); err != nil {
			return err
		}
	}
	stream := rlp.NewStream(reader, 0)

	// Import the preimages in batches to prevent disk thrashing
	preimages := make(map[common.Hash][]byte)

	for {
		// Read the next entry and ensure it's not junk
		var blob []byte

		if err := stream.Decode(&blob); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		// Accumulate the preimages and flush when enough ws gathered
		preimages[crypto.Keccak256Hash(blob)] = common.CopyBytes(blob)
		if len(preimages) > 1024 {
			rawdb.WritePreimages(db, preimages)
			preimages = make(map[common.Hash][]byte)
		}
	}
	// Flush the last batch preimage data
	if len(preimages) > 0 {
		rawdb.WritePreimages(db, preimages)
	}
	return nil
}

// 导出所有已知的哈希预映射到指定文件，
//截断文件中已经存在的任何数据。
//这是未来不推荐的功能的一部分。
func ExportPreimages(db ethdb.Database, fn string) error {
	log.Info("Exporting preimages", "file", fn)

	// Open the file handle and potentially wrap with a gzip stream
	fh, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer fh.Close()

	var writer io.Writer = fh
	if strings.HasSuffix(fn, ".gz") {
		writer = gzip.NewWriter(writer)
		defer writer.(*gzip.Writer).Close()
	}
	// Iterate over the preimages and export them
	it := db.NewIterator([]byte("secure-key-"), nil)
	defer it.Release()

	for it.Next() {
		if err := rlp.Encode(writer, it.Value()); err != nil {
			return err
		}
	}
	log.Info("Exported preimages", "file", fn)
	return nil
}

//Exportheader用于导出/进口流中。当我们进行出口时，
//我们输出的第一个元素是Exportheader。
//每当进行后退兼容的更改时，版本标题
//应该碰撞。
//如果进口商看到更高版本，则应拒绝导入。
type exportHeader struct {
	Magic    string // Always set to 'gethdbdump' for disambiguation
	Version  uint64
	Kind     string
	UnixTime uint64
}

const exportMagic = "gethdbdump"
const (
	OpBatchAdd = 0
	OpBatchDel = 1
)

// ExportldBdata将一批快照数据导入到数据库中
func ImportLDBData(db ethdb.Database, f string, startIndex int64, interrupt chan struct{}) error {
	log.Info("Importing leveldb data", "file", f)

	// Open the file handle and potentially unwrap the gzip stream
	fh, err := os.Open(f)
	if err != nil {
		return err
	}
	defer fh.Close()

	var reader io.Reader = bufio.NewReader(fh)
	if strings.HasSuffix(f, ".gz") {
		if reader, err = gzip.NewReader(reader); err != nil {
			return err
		}
	}
	stream := rlp.NewStream(reader, 0)

	// Read the header
	var header exportHeader
	if err := stream.Decode(&header); err != nil {
		return fmt.Errorf("could not decode header: %v", err)
	}
	if header.Magic != exportMagic {
		return errors.New("incompatible data, wrong magic")
	}
	if header.Version != 0 {
		return fmt.Errorf("incompatible version %d, (support only 0)", header.Version)
	}
	log.Info("Importing data", "file", f, "type", header.Kind, "data age",
		common.PrettyDuration(time.Since(time.Unix(int64(header.UnixTime), 0))))

	// Import the snapshot in batches to prevent disk thrashing
	var (
		count  int64
		start  = time.Now()
		logged = time.Now()
		batch  = db.NewBatch()
	)
	for {
		// Read the next entry
		var (
			op       byte
			key, val []byte
		)
		if err := stream.Decode(&op); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if err := stream.Decode(&key); err != nil {
			return err
		}
		if err := stream.Decode(&val); err != nil {
			return err
		}
		if count < startIndex {
			count++
			continue
		}
		switch op {
		case OpBatchDel:
			batch.Delete(key)
		case OpBatchAdd:
			batch.Put(key, val)
		default:
			return fmt.Errorf("unknown op %d\n", op)
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
		// Check interruption emitted by ctrl+c
		if count%1000 == 0 {
			select {
			case <-interrupt:
				if err := batch.Write(); err != nil {
					return err
				}
				log.Info("External data import interrupted", "file", f, "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
				return nil
			default:
			}
		}
		if count%1000 == 0 && time.Since(logged) > 8*time.Second {
			log.Info("Importing external data", "file", f, "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
		count += 1
	}
	// Flush the last batch snapshot data
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return err
		}
	}
	log.Info("Imported chain data", "file", f, "count", count,
		"elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// Chaindaiterator是一个接口包装的所有必要功能以迭代
//导出链数据。
type ChainDataIterator interface {
	// Next returns the key-value pair for next exporting entry in the iterator.
	// When the end is reached, it will return (0, nil, nil, false).
	Next() (byte, []byte, []byte, bool)

	// Release releases associated resources. Release should always succeed and can
	// be called multiple times without causing error.
	Release()
}

// ExportChainData导出给定的数据类型（截断所有已经存在的数据）
//在文件中。如果后缀为“ GZ”，则使用GZIP压缩。
func ExportChaindata(fn string, kind string, iter ChainDataIterator, interrupt chan struct{}) error {
	log.Info("Exporting chain data", "file", fn, "kind", kind)
	defer iter.Release()

	// Open the file handle and potentially wrap with a gzip stream
	fh, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer fh.Close()

	var writer io.Writer = fh
	if strings.HasSuffix(fn, ".gz") {
		writer = gzip.NewWriter(writer)
		defer writer.(*gzip.Writer).Close()
	}
	// Write the header
	if err := rlp.Encode(writer, &exportHeader{
		Magic:    exportMagic,
		Version:  0,
		Kind:     kind,
		UnixTime: uint64(time.Now().Unix()),
	}); err != nil {
		return err
	}
	// Extract data from source iterator and dump them out to file
	var (
		count  int64
		start  = time.Now()
		logged = time.Now()
	)
	for {
		op, key, val, ok := iter.Next()
		if !ok {
			break
		}
		if err := rlp.Encode(writer, op); err != nil {
			return err
		}
		if err := rlp.Encode(writer, key); err != nil {
			return err
		}
		if err := rlp.Encode(writer, val); err != nil {
			return err
		}
		if count%1000 == 0 {
			// Check interruption emitted by ctrl+c
			select {
			case <-interrupt:
				log.Info("Chain data exporting interrupted", "file", fn,
					"kind", kind, "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
				return nil
			default:
			}
			if time.Since(logged) > 8*time.Second {
				log.Info("Exporting chain data", "file", fn, "kind", kind,
					"count", count, "elapsed", common.PrettyDuration(time.Since(start)))
				logged = time.Now()
			}
		}
		count++
	}
	log.Info("Exported chain data", "file", fn, "kind", kind, "count", count,
		"elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}
