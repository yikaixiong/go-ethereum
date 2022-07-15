//版权2021 The Go-Ethereum作者
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

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/console/prompt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli/v2"
)

var (
	removedbCommand = &cli.Command{
		Action:    removeDB,
		Name:      "removedb",
		Usage:     "Remove blockchain and state databases",
		ArgsUsage: "",
		Flags:     utils.DatabasePathFlags,
		Description: `
Remove blockchain and state databases`,
	}
	dbCommand = &cli.Command{
		Name:      "db",
		Usage:     "Low level database operations",
		ArgsUsage: "",
		Subcommands: []*cli.Command{
			dbInspectCmd,
			dbStatCmd,
			dbCompactCmd,
			dbGetCmd,
			dbDeleteCmd,
			dbPutCmd,
			dbGetSlotsCmd,
			dbDumpFreezerIndex,
			dbImportCmd,
			dbExportCmd,
			dbMetadataCmd,
			dbMigrateFreezerCmd,
			dbCheckStateContentCmd,
		},
	}
	dbInspectCmd = &cli.Command{
		Action:    inspect,
		Name:      "inspect",
		ArgsUsage: "<prefix> <start>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Usage:       "Inspect the storage size for each type of data in the database",
		Description: `This commands iterates the entire database. If the optional 'prefix' and 'start' arguments are provided, then the iteration is limited to the given subset of data.`,
	}
	dbCheckStateContentCmd = &cli.Command{
		Action:    checkStateContent,
		Name:      "check-state-content",
		ArgsUsage: "<start (optional)>",
		Flags:     utils.GroupFlags(utils.NetworkFlags, utils.DatabasePathFlags),
		Usage:     "Verify that state data is cryptographically correct",
		Description: `This command iterates the entire database for 32-byte keys, looking for rlp-encoded trie nodes.
For each trie node encountered, it checks that the key corresponds to the keccak256(value). If this is not true, this indicates
a data corruption.`,
	}
	dbStatCmd = &cli.Command{
		Action: dbStats,
		Name:   "stats",
		Usage:  "Print leveldb statistics",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
	}
	dbCompactCmd = &cli.Command{
		Action: dbCompact,
		Name:   "compact",
		Usage:  "Compact leveldb database. WARNING: May take a very long time",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
			utils.CacheFlag,
			utils.CacheDatabaseFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: `This command performs a database compaction. 
WARNING: This operation may take a very long time to finish, and may cause database
corruption if it is aborted during execution'!`,
	}
	dbGetCmd = &cli.Command{
		Action:    dbGet,
		Name:      "get",
		Usage:     "Show the value of a database key",
		ArgsUsage: "<hex-encoded key>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "This command looks up the specified database key from the database.",
	}
	dbDeleteCmd = &cli.Command{
		Action:    dbDelete,
		Name:      "delete",
		Usage:     "Delete a database key (WARNING: may corrupt your database)",
		ArgsUsage: "<hex-encoded key>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: `This command deletes the specified database key from the database. 
WARNING: This is a low-level operation which may cause database corruption!`,
	}
	dbPutCmd = &cli.Command{
		Action:    dbPut,
		Name:      "put",
		Usage:     "Set the value of a database key (WARNING: may corrupt your database)",
		ArgsUsage: "<hex-encoded key> <hex-encoded value>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: `This command sets a given database key to the given value. 
WARNING: This is a low-level operation which may cause database corruption!`,
	}
	dbGetSlotsCmd = &cli.Command{
		Action:    dbDumpTrie,
		Name:      "dumptrie",
		Usage:     "Show the storage key/values of a given storage trie",
		ArgsUsage: "<hex-encoded storage trie root> <hex-encoded start (optional)> <int max elements (optional)>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "This command looks up the specified database key from the database.",
	}
	dbDumpFreezerIndex = &cli.Command{
		Action:    freezerInspect,
		Name:      "freezer-index",
		Usage:     "Dump out the index of a given freezer type",
		ArgsUsage: "<type> <start (int)> <end (int)>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "This command displays information about the freezer index.",
	}
	dbImportCmd = &cli.Command{
		Action:    importLDBdata,
		Name:      "import",
		Usage:     "Imports leveldb-data from an exported RLP dump.",
		ArgsUsage: "<dumpfile> <start (optional)",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "The import command imports the specific chain data from an RLP encoded stream.",
	}
	dbExportCmd = &cli.Command{
		Action:    exportChaindata,
		Name:      "export",
		Usage:     "Exports the chain data into an RLP dump. If the <dumpfile> has .gz suffix, gzip compression will be used.",
		ArgsUsage: "<type> <dumpfile>",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "Exports the specified chain data to an RLP encoded stream, optionally gzip-compressed.",
	}
	dbMetadataCmd = &cli.Command{
		Action: showMetaData,
		Name:   "metadata",
		Usage:  "Shows metadata about the chain status.",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: "Shows metadata about the chain status.",
	}
	dbMigrateFreezerCmd = &cli.Command{
		Action:    freezerMigrate,
		Name:      "freezer-migrate",
		Usage:     "Migrate legacy parts of the freezer. (WARNING: may take a long time)",
		ArgsUsage: "",
		Flags: utils.GroupFlags([]cli.Flag{
			utils.SyncModeFlag,
		}, utils.NetworkFlags, utils.DatabasePathFlags),
		Description: `The freezer-migrate command checks your database for receipts in a legacy format and updates those.
WARNING: please back-up the receipt files in your ancients before running this command.`,
	}
)

// 移除db数据库
func removeDB(ctx *cli.Context) error {
	// 获取节点的配置信息
	stack, config := makeConfigNode(ctx)

	// 删除完整的节点状态数据库
	path := stack.ResolvePath("chaindata")
	// 根据数据库路径做判断
	if common.FileExist(path) {
		// 确定并且删除节点状态数据库
		confirmAndRemoveDB(path, "full node state database")
	} else {
		log.Info("Full node state database missing", "path", path)
	}
	// 删除完整的节点古代数据库
	path = config.Eth.DatabaseFreezer
	switch {
	case path == "":
		path = filepath.Join(stack.ResolvePath("chaindata"), "ancient")
	case !filepath.IsAbs(path):
		path = config.Node.ResolvePath(path)
	}
	if common.FileExist(path) {
		confirmAndRemoveDB(path, "full node ancient database")
	} else {
		log.Info("Full node ancient database missing", "path", path)
	}
	// 删除光节点数据库
	path = stack.ResolvePath("lightchaindata")
	if common.FileExist(path) {
		confirmAndRemoveDB(path, "light node database")
	} else {
		log.Info("Light node database missing", "path", path)
	}
	return nil
}

// confirmAndRemovedB提示用户进行最后确认并删除
//文件夹如果接受。
func confirmAndRemoveDB(database string, kind string) {
	confirm, err := prompt.Stdin.PromptConfirm(fmt.Sprintf("Remove %s (%s)?", kind, database))
	switch {
	case err != nil:
		utils.Fatalf("%v", err)
	case !confirm:
		log.Info("Database deletion skipped", "path", database)
	default:
		start := time.Now()
		filepath.Walk(database, func(path string, info os.FileInfo, err error) error {
			// 如果我们在顶级文件夹中，请重复进入
			if path == database {
				return nil
			}
			// 删除所有文件，但没有删除子文件夹
			if !info.IsDir() {
				os.Remove(path)
				return nil
			}
			return filepath.SkipDir
		})
		log.Info("Database successfully deleted", "path", database, "elapsed", common.PrettyDuration(time.Since(start)))
	}
}

//检查
func inspect(ctx *cli.Context) error {
	var (
		prefix []byte
		start  []byte
	)
	if ctx.NArg() > 2 {
		return fmt.Errorf("Max 2 arguments: %v", ctx.Command.ArgsUsage)
	}
	if ctx.NArg() >= 1 {
		if d, err := hexutil.Decode(ctx.Args().Get(0)); err != nil {
			return fmt.Errorf("failed to hex-decode 'prefix': %v", err)
		} else {
			prefix = d
		}
	}
	if ctx.NArg() >= 2 {
		if d, err := hexutil.Decode(ctx.Args().Get(1)); err != nil {
			return fmt.Errorf("failed to hex-decode 'start': %v", err)
		} else {
			start = d
		}
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()

	return rawdb.InspectDatabase(db, prefix, start)
}

func checkStateContent(ctx *cli.Context) error {
	var (
		prefix []byte
		start  []byte
	)
	if ctx.NArg() > 1 {
		return fmt.Errorf("max 1 argument: %v", ctx.Command.ArgsUsage)
	}
	if ctx.NArg() > 0 {
		if d, err := hexutil.Decode(ctx.Args().First()); err != nil {
			return fmt.Errorf("failed to hex-decode 'start': %v", err)
		} else {
			start = d
		}
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()
	var (
		it        = rawdb.NewKeyLengthIterator(db.NewIterator(prefix, start), 32)
		hasher    = crypto.NewKeccakState()
		got       = make([]byte, 32)
		errs      int
		count     int
		startTime = time.Now()
		lastLog   = time.Now()
	)
	for it.Next() {
		count++
		k := it.Key()
		v := it.Value()
		hasher.Reset()
		hasher.Write(v)
		hasher.Read(got)
		if !bytes.Equal(k, got) {
			errs++
			fmt.Printf("Error at %#x\n", k)
			fmt.Printf("  Hash:  %#x\n", got)
			fmt.Printf("  Data:  %#x\n", v)
		}
		if time.Since(lastLog) > 8*time.Second {
			log.Info("Iterating the database", "at", fmt.Sprintf("%#x", k), "elapsed", common.PrettyDuration(time.Since(startTime)))
			lastLog = time.Now()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	log.Info("Iterated the state content", "errors", errs, "items", count)
	return nil
}

func showLeveldbStats(db ethdb.KeyValueStater) {
	if stats, err := db.Stat("leveldb.stats"); err != nil {
		log.Warn("Failed to read database stats", "error", err)
	} else {
		fmt.Println(stats)
	}
	if ioStats, err := db.Stat("leveldb.iostats"); err != nil {
		log.Warn("Failed to read database iostats", "error", err)
	} else {
		fmt.Println(ioStats)
	}
}

// 获取数据库的状态
func dbStats(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()

	showLeveldbStats(db)
	return nil
}

func dbCompact(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	defer db.Close()

	log.Info("Stats before compaction")
	showLeveldbStats(db)

	log.Info("Triggering compaction")
	if err := db.Compact(nil, nil); err != nil {
		log.Info("Compact err", "error", err)
		return err
	}
	log.Info("Stats after compaction")
	showLeveldbStats(db)
	return nil
}

// DBGET显示给定数据库密钥的值
func dbGet(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()

	key, err := common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}

	data, err := db.Get(key)
	if err != nil {
		log.Info("Get operation failed", "key", fmt.Sprintf("0x%#x", key), "error", err)
		return err
	}
	fmt.Printf("key %#x: %#x\n", key, data)
	return nil
}

// DBDELETE从数据库中删除键
func dbDelete(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	defer db.Close()

	key, err := common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}
	data, err := db.Get(key)
	if err == nil {
		fmt.Printf("Previous value: %#x\n", data)
	}
	if err = db.Delete(key); err != nil {
		log.Info("Delete operation returned an error", "key", fmt.Sprintf("0x%#x", key), "error", err)
		return err
	}
	return nil
}

// dbput覆盖数据库中的值
func dbPut(ctx *cli.Context) error {
	if ctx.NArg() != 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	defer db.Close()

	var (
		key   []byte
		value []byte
		data  []byte
		err   error
	)
	key, err = common.ParseHexOrString(ctx.Args().Get(0))
	if err != nil {
		log.Info("Could not decode the key", "error", err)
		return err
	}
	value, err = hexutil.Decode(ctx.Args().Get(1))
	if err != nil {
		log.Info("Could not decode the value", "error", err)
		return err
	}
	data, err = db.Get(key)
	if err == nil {
		fmt.Printf("Previous value: %#x\n", data)
	}
	return db.Put(key, value)
}

// dbdumptrie显示给定存储trie的键值插槽
func dbDumpTrie(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, true)
	defer db.Close()
	var (
		root  []byte
		start []byte
		max   = int64(-1)
		err   error
	)
	if root, err = hexutil.Decode(ctx.Args().Get(0)); err != nil {
		log.Info("Could not decode the root", "error", err)
		return err
	}
	stRoot := common.BytesToHash(root)
	if ctx.NArg() >= 2 {
		if start, err = hexutil.Decode(ctx.Args().Get(1)); err != nil {
			log.Info("Could not decode the seek position", "error", err)
			return err
		}
	}
	if ctx.NArg() >= 3 {
		if max, err = strconv.ParseInt(ctx.Args().Get(2), 10, 64); err != nil {
			log.Info("Could not decode the max count", "error", err)
			return err
		}
	}
	theTrie, err := trie.New(common.Hash{}, stRoot, trie.NewDatabase(db))
	if err != nil {
		return err
	}
	var count int64
	it := trie.NewIterator(theTrie.NodeIterator(start))
	for it.Next() {
		if max > 0 && count == max {
			fmt.Printf("Exiting after %d values\n", count)
			break
		}
		fmt.Printf("  %d. key %#x: %#x\n", count, it.Key, it.Value)
		count++
	}
	return it.Err
}

func freezerInspect(ctx *cli.Context) error {
	var (
		start, end    int64
		disableSnappy bool
		err           error
	)
	if ctx.NArg() < 3 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	kind := ctx.Args().Get(0)
	if noSnap, ok := rawdb.FreezerNoSnappy[kind]; !ok {
		var options []string
		for opt := range rawdb.FreezerNoSnappy {
			options = append(options, opt)
		}
		sort.Strings(options)
		return fmt.Errorf("Could read freezer-type '%v'. Available options: %v", kind, options)
	} else {
		disableSnappy = noSnap
	}
	if start, err = strconv.ParseInt(ctx.Args().Get(1), 10, 64); err != nil {
		log.Info("Could read start-param", "error", err)
		return err
	}
	if end, err = strconv.ParseInt(ctx.Args().Get(2), 10, 64); err != nil {
		log.Info("Could read count param", "error", err)
		return err
	}
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()
	path := filepath.Join(stack.ResolvePath("chaindata"), "ancient")
	log.Info("Opening freezer", "location", path, "name", kind)
	if f, err := rawdb.NewFreezerTable(path, kind, disableSnappy, true); err != nil {
		return err
	} else {
		f.DumpIndex(start, end)
	}
	return nil
}

// 插入数据库数据
func importLDBdata(ctx *cli.Context) error {
	start := 0
	switch ctx.NArg() {
	case 1:
		break
	case 2:
		s, err := strconv.Atoi(ctx.Args().Get(1))
		if err != nil {
			return fmt.Errorf("second arg must be an integer: %v", err)
		}
		start = s
	default:
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	var (
		fName     = ctx.Args().Get(0)
		stack, _  = makeConfigNode(ctx)
		interrupt = make(chan os.Signal, 1)
		stop      = make(chan struct{})
	)
	defer stack.Close()
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	defer close(interrupt)
	go func() {
		if _, ok := <-interrupt; ok {
			log.Info("Interrupted during ldb import, stopping at next batch")
		}
		close(stop)
	}()
	db := utils.MakeChainDatabase(ctx, stack, false)
	return utils.ImportLDBData(db, fName, int64(start), stop)
}

type preimageIterator struct {
	iter ethdb.Iterator
}

func (iter *preimageIterator) Next() (byte, []byte, []byte, bool) {
	for iter.iter.Next() {
		key := iter.iter.Key()
		if bytes.HasPrefix(key, rawdb.PreimagePrefix) && len(key) == (len(rawdb.PreimagePrefix)+common.HashLength) {
			return utils.OpBatchAdd, key, iter.iter.Value(), true
		}
	}
	return 0, nil, nil, false
}

func (iter *preimageIterator) Release() {
	iter.iter.Release()
}

type snapshotIterator struct {
	init    bool
	account ethdb.Iterator
	storage ethdb.Iterator
}

func (iter *snapshotIterator) Next() (byte, []byte, []byte, bool) {
	if !iter.init {
		iter.init = true
		return utils.OpBatchDel, rawdb.SnapshotRootKey, nil, true
	}
	for iter.account.Next() {
		key := iter.account.Key()
		if bytes.HasPrefix(key, rawdb.SnapshotAccountPrefix) && len(key) == (len(rawdb.SnapshotAccountPrefix)+common.HashLength) {
			return utils.OpBatchAdd, key, iter.account.Value(), true
		}
	}
	for iter.storage.Next() {
		key := iter.storage.Key()
		if bytes.HasPrefix(key, rawdb.SnapshotStoragePrefix) && len(key) == (len(rawdb.SnapshotStoragePrefix)+2*common.HashLength) {
			return utils.OpBatchAdd, key, iter.storage.Value(), true
		}
	}
	return 0, nil, nil, false
}

func (iter *snapshotIterator) Release() {
	iter.account.Release()
	iter.storage.Release()
}

// Chainexporter定义了所有可导出链数据的导出方案。
var chainExporters = map[string]func(db ethdb.Database) utils.ChainDataIterator{
	"preimage": func(db ethdb.Database) utils.ChainDataIterator {
		iter := db.NewIterator(rawdb.PreimagePrefix, nil)
		return &preimageIterator{iter: iter}
	},
	"snapshot": func(db ethdb.Database) utils.ChainDataIterator {
		account := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
		storage := db.NewIterator(rawdb.SnapshotStoragePrefix, nil)
		return &snapshotIterator{account: account, storage: storage}
	},
}

func exportChaindata(ctx *cli.Context) error {
	if ctx.NArg() < 2 {
		return fmt.Errorf("required arguments: %v", ctx.Command.ArgsUsage)
	}
	// 解析所需的链数据类型，请确保受支持。
	kind := ctx.Args().Get(0)
	kind = strings.ToLower(strings.Trim(kind, " "))
	exporter, ok := chainExporters[kind]
	if !ok {
		var kinds []string
		for kind := range chainExporters {
			kinds = append(kinds, kind)
		}
		return fmt.Errorf("invalid data type %s, supported types: %s", kind, strings.Join(kinds, ", "))
	}
	var (
		stack, _  = makeConfigNode(ctx)
		interrupt = make(chan os.Signal, 1)
		stop      = make(chan struct{})
	)
	defer stack.Close()
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	defer close(interrupt)
	go func() {
		if _, ok := <-interrupt; ok {
			log.Info("Interrupted during db export, stopping at next batch")
		}
		close(stop)
	}()
	db := utils.MakeChainDatabase(ctx, stack, true)
	return utils.ExportChaindata(ctx.Args().Get(1), kind, exporter(db), stop)
}

func showMetaData(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()
	db := utils.MakeChainDatabase(ctx, stack, true)
	ancients, err := db.Ancients()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing ancients: %v", err)
	}
	pp := func(val *uint64) string {
		if val == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%d (%#x)", *val, *val)
	}
	data := [][]string{
		{"databaseVersion", pp(rawdb.ReadDatabaseVersion(db))},
		{"headBlockHash", fmt.Sprintf("%v", rawdb.ReadHeadBlockHash(db))},
		{"headFastBlockHash", fmt.Sprintf("%v", rawdb.ReadHeadFastBlockHash(db))},
		{"headHeaderHash", fmt.Sprintf("%v", rawdb.ReadHeadHeaderHash(db))}}
	if b := rawdb.ReadHeadBlock(db); b != nil {
		data = append(data, []string{"headBlock.Hash", fmt.Sprintf("%v", b.Hash())})
		data = append(data, []string{"headBlock.Root", fmt.Sprintf("%v", b.Root())})
		data = append(data, []string{"headBlock.Number", fmt.Sprintf("%d (%#x)", b.Number(), b.Number())})
	}
	if b := rawdb.ReadSkeletonSyncStatus(db); b != nil {
		data = append(data, []string{"SkeletonSyncStatus", string(b)})
	}
	if h := rawdb.ReadHeadHeader(db); h != nil {
		data = append(data, []string{"headHeader.Hash", fmt.Sprintf("%v", h.Hash())})
		data = append(data, []string{"headHeader.Root", fmt.Sprintf("%v", h.Root)})
		data = append(data, []string{"headHeader.Number", fmt.Sprintf("%d (%#x)", h.Number, h.Number)})
	}
	data = append(data, [][]string{{"frozen", fmt.Sprintf("%d items", ancients)},
		{"lastPivotNumber", pp(rawdb.ReadLastPivotNumber(db))},
		{"len(snapshotSyncStatus)", fmt.Sprintf("%d bytes", len(rawdb.ReadSnapshotSyncStatus(db)))},
		{"snapshotGenerator", snapshot.ParseGeneratorStatus(rawdb.ReadSnapshotGenerator(db))},
		{"snapshotDisabled", fmt.Sprintf("%v", rawdb.ReadSnapshotDisabled(db))},
		{"snapshotJournal", fmt.Sprintf("%d bytes", len(rawdb.ReadSnapshotJournal(db)))},
		{"snapshotRecoveryNumber", pp(rawdb.ReadSnapshotRecoveryNumber(db))},
		{"snapshotRoot", fmt.Sprintf("%v", rawdb.ReadSnapshotRoot(db))},
		{"txIndexTail", pp(rawdb.ReadTxIndexTail(db))},
		{"fastTxLookupLimit", pp(rawdb.ReadFastTxLookupLimit(db))},
	}...)
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Field", "Value"})
	table.AppendBulk(data)
	table.Render()
	return nil
}

func freezerMigrate(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	db := utils.MakeChainDatabase(ctx, stack, false)
	defer db.Close()

	// Check first block for legacy receipt format
	numAncients, err := db.Ancients()
	if err != nil {
		return err
	}
	if numAncients < 1 {
		log.Info("No receipts in freezer to migrate")
		return nil
	}

	isFirstLegacy, firstIdx, err := dbHasLegacyReceipts(db, 0)
	if err != nil {
		return err
	}
	if !isFirstLegacy {
		log.Info("No legacy receipts to migrate")
		return nil
	}

	log.Info("Starting migration", "ancients", numAncients, "firstLegacy", firstIdx)
	start := time.Now()
	if err := db.MigrateTable("receipts", types.ConvertLegacyStoredReceipts); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	log.Info("Migration finished", "duration", time.Since(start))

	return nil
}

// DBHASLEGACYRECEIPTS检查冰柜条目是否有旧收据。它停在第一个
//非空收据并检查其格式。第一个非空元素的索引是
//第二个返回参数。
func dbHasLegacyReceipts(db ethdb.Database, firstIdx uint64) (bool, uint64, error) {
	// 检查第一个块以获取遗产收据格式
	numAncients, err := db.Ancients()
	if err != nil {
		return false, 0, err
	}
	if numAncients < 1 {
		return false, 0, nil
	}
	if firstIdx >= numAncients {
		return false, firstIdx, nil
	}
	var (
		legacy       bool
		blob         []byte
		emptyRLPList = []byte{192}
	)
	// 只有在
	//尚未提供索引。
	if firstIdx == 0 {
		for i := uint64(0); i < numAncients; i++ {
			blob, err = db.Ancient("receipts", i)
			if err != nil {
				return false, 0, err
			}
			if len(blob) == 0 {
				continue
			}
			if !bytes.Equal(blob, emptyRLPList) {
				firstIdx = i
				break
			}
		}
	}
	// Is first non-empty receipt legacy?
	first, err := db.Ancient("receipts", firstIdx)
	if err != nil {
		return false, 0, err
	}
	legacy, err = types.IsLegacyStoredReceipts(first)
	return legacy, firstIdx, err
}
