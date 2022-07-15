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

// Geth是以太坊的官方指挥行客户。
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/console/prompt"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/node"

	// 强制负载示踪剂发动机触发注册
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"

	"github.com/urfave/cli/v2"
)

const (
	clientIdentifier = "geth" // 客户标识符通过网络做广告
)

// 定义变量
var (
	// git sha1提交版本的哈希（通过链接标志设置）
	gitCommit = ""
	gitDate   = ""
	// 包含所有命令和标志的应用程序。
	app = flags.NewApp(gitCommit, gitDate, "the go-ethereum command line interface")
	// 配置节点的标志
	nodeFlags = utils.GroupFlags([]cli.Flag{
		utils.IdentityFlag,
		utils.UnlockedAccountFlag,
		utils.PasswordFileFlag,
		utils.BootnodesFlag,
		utils.MinFreeDiskSpaceFlag,
		utils.KeyStoreDirFlag,
		utils.ExternalSignerFlag,
		utils.NoUSBFlag,
		utils.USBFlag,
		utils.SmartCardDaemonPathFlag,
		utils.OverrideGrayGlacierFlag,
		utils.OverrideTerminalTotalDifficulty,
		utils.EthashCacheDirFlag,
		utils.EthashCachesInMemoryFlag,
		utils.EthashCachesOnDiskFlag,
		utils.EthashCachesLockMmapFlag,
		utils.EthashDatasetDirFlag,
		utils.EthashDatasetsInMemoryFlag,
		utils.EthashDatasetsOnDiskFlag,
		utils.EthashDatasetsLockMmapFlag,
		utils.TxPoolLocalsFlag,
		utils.TxPoolNoLocalsFlag,
		utils.TxPoolJournalFlag,
		utils.TxPoolRejournalFlag,
		utils.TxPoolPriceLimitFlag,
		utils.TxPoolPriceBumpFlag,
		utils.TxPoolAccountSlotsFlag,
		utils.TxPoolGlobalSlotsFlag,
		utils.TxPoolAccountQueueFlag,
		utils.TxPoolGlobalQueueFlag,
		utils.TxPoolLifetimeFlag,
		utils.SyncModeFlag,
		utils.ExitWhenSyncedFlag,
		utils.GCModeFlag,
		utils.SnapshotFlag,
		utils.TxLookupLimitFlag,
		utils.LightServeFlag,
		utils.LightIngressFlag,
		utils.LightEgressFlag,
		utils.LightMaxPeersFlag,
		utils.LightNoPruneFlag,
		utils.LightKDFFlag,
		utils.UltraLightServersFlag,
		utils.UltraLightFractionFlag,
		utils.UltraLightOnlyAnnounceFlag,
		utils.LightNoSyncServeFlag,
		utils.EthRequiredBlocksFlag,
		utils.LegacyWhitelistFlag,
		utils.BloomFilterSizeFlag,
		utils.CacheFlag,
		utils.CacheDatabaseFlag,
		utils.CacheTrieFlag,
		utils.CacheTrieJournalFlag,
		utils.CacheTrieRejournalFlag,
		utils.CacheGCFlag,
		utils.CacheSnapshotFlag,
		utils.CacheNoPrefetchFlag,
		utils.CachePreimagesFlag,
		utils.FDLimitFlag,
		utils.ListenPortFlag,
		utils.DiscoveryPortFlag,
		utils.MaxPeersFlag,
		utils.MaxPendingPeersFlag,
		utils.MiningEnabledFlag,
		utils.MinerThreadsFlag,
		utils.MinerNotifyFlag,
		utils.LegacyMinerGasTargetFlag,
		utils.MinerGasLimitFlag,
		utils.MinerGasPriceFlag,
		utils.MinerEtherbaseFlag,
		utils.MinerExtraDataFlag,
		utils.MinerRecommitIntervalFlag,
		utils.MinerNoVerifyFlag,
		utils.NATFlag,
		utils.NoDiscoverFlag,
		utils.DiscoveryV5Flag,
		utils.NetrestrictFlag,
		utils.NodeKeyFileFlag,
		utils.NodeKeyHexFlag,
		utils.DNSDiscoveryFlag,
		utils.DeveloperFlag,
		utils.DeveloperPeriodFlag,
		utils.DeveloperGasLimitFlag,
		utils.VMEnableDebugFlag,
		utils.NetworkIdFlag,
		utils.EthStatsURLFlag,
		utils.FakePoWFlag,
		utils.NoCompactionFlag,
		utils.GpoBlocksFlag,
		utils.GpoPercentileFlag,
		utils.GpoMaxGasPriceFlag,
		utils.GpoIgnoreGasPriceFlag,
		utils.MinerNotifyFullFlag,
		utils.IgnoreLegacyReceiptsFlag,
		configFileFlag,
	}, utils.NetworkFlags, utils.DatabasePathFlags)

	rpcFlags = []cli.Flag{
		utils.HTTPEnabledFlag,
		utils.HTTPListenAddrFlag,
		utils.HTTPPortFlag,
		utils.HTTPCORSDomainFlag,
		utils.AuthListenFlag,
		utils.AuthPortFlag,
		utils.AuthVirtualHostsFlag,
		utils.JWTSecretFlag,
		utils.HTTPVirtualHostsFlag,
		utils.GraphQLEnabledFlag,
		utils.GraphQLCORSDomainFlag,
		utils.GraphQLVirtualHostsFlag,
		utils.HTTPApiFlag,
		utils.HTTPPathPrefixFlag,
		utils.WSEnabledFlag,
		utils.WSListenAddrFlag,
		utils.WSPortFlag,
		utils.WSApiFlag,
		utils.WSAllowedOriginsFlag,
		utils.WSPathPrefixFlag,
		utils.IPCDisabledFlag,
		utils.IPCPathFlag,
		utils.InsecureUnlockAllowedFlag,
		utils.RPCGlobalGasCapFlag,
		utils.RPCGlobalEVMTimeoutFlag,
		utils.RPCGlobalTxFeeCapFlag,
		utils.AllowUnprotectedTxs,
	}

	metricsFlags = []cli.Flag{
		utils.MetricsEnabledFlag,
		utils.MetricsEnabledExpensiveFlag,
		utils.MetricsHTTPFlag,
		utils.MetricsPortFlag,
		utils.MetricsEnableInfluxDBFlag,
		utils.MetricsInfluxDBEndpointFlag,
		utils.MetricsInfluxDBDatabaseFlag,
		utils.MetricsInfluxDBUsernameFlag,
		utils.MetricsInfluxDBPasswordFlag,
		utils.MetricsInfluxDBTagsFlag,
		utils.MetricsEnableInfluxDBV2Flag,
		utils.MetricsInfluxDBTokenFlag,
		utils.MetricsInfluxDBBucketFlag,
		utils.MetricsInfluxDBOrganizationFlag,
	}
)

// TODO: 初始化，把传入的参数实例化
func init() {
	// 初始化CLI应用程序并启动Geth
	app.Action = geth
	app.HideVersion = true // 我们有打印版本的命令
	app.Copyright = "Copyright 2013-2022 The go-ethereum Authors"
	app.Commands = []*cli.Command{
		// See chaincmd.go:
		initCommand,
		importCommand,
		exportCommand,
		importPreimagesCommand,
		exportPreimagesCommand,
		removedbCommand,
		dumpCommand,
		dumpGenesisCommand,
		// See accountcmd.go:
		accountCommand,
		walletCommand,
		// See consolecmd.go:
		consoleCommand,
		attachCommand,
		javascriptCommand,
		// See misccmd.go:
		makecacheCommand,
		makedagCommand,
		versionCommand,
		versionCheckCommand,
		licenseCommand,
		// See config.go
		dumpConfigCommand,
		// see dbcmd.go
		dbCommand,
		// See cmd/utils/flags_legacy.go
		utils.ShowDeprecated,
		// See snapshot.go
		snapshotCommand,
	}
	sort.Sort(cli.CommandsByName(app.Commands))

	app.Flags = utils.GroupFlags(
		nodeFlags,
		rpcFlags,
		consoleFlags,
		debug.Flags,
		metricsFlags,
	)

	app.Before = func(ctx *cli.Context) error {
		flags.MigrateGlobalFlags(ctx)
		return debug.Setup(ctx)
	}
	app.After = func(ctx *cli.Context) error {
		debug.Exit()
		prompt.Stdin.Close() // Resets terminal mode.
		return nil
	}
}

// 启动
func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

//准备操纵内存缓存津贴和设置度量系统。
//在启动DEVP2P堆栈之前，应调用此功能。
func prepare(ctx *cli.Context) {
	// 如果我们运行已知的预设，请为方便起见。
	switch {
	case ctx.IsSet(utils.RopstenFlag.Name):
		log.Info("Starting Geth on Ropsten testnet...")

	case ctx.IsSet(utils.RinkebyFlag.Name):
		log.Info("Starting Geth on Rinkeby testnet...")

	case ctx.IsSet(utils.GoerliFlag.Name):
		log.Info("Starting Geth on Görli testnet...")

	case ctx.IsSet(utils.SepoliaFlag.Name):
		log.Info("Starting Geth on Sepolia testnet...")

	case ctx.IsSet(utils.KilnFlag.Name):
		log.Info("Starting Geth on Kiln testnet...")

	case ctx.IsSet(utils.DeveloperFlag.Name):
		log.Info("Starting Geth in ephemeral dev mode...")
		log.Warn(`You are running Geth in --dev mode. Please note the following:

  1.此模式仅用于快速的迭代发展，而无需假设
     安全性或持久性。
  2.除非另有说明，否则数据库是在内存中创建的。因此，关闭
     您的计算机或损失电源将擦除您的整个块数据和链状态
     您的开发环境。
  3.将提供一个随机的，预先分配的开发人员帐户，并解锁为
     eth.coinbase，可用于测试。随机开发帐户是暂时的
     存储在ramdisk上，如果您的机器重新启动，将会丢失。
  4.默认情况下启用挖掘。但是，客户仅在交易时才密封块
     在孟买待定。矿工最低接受的天然气价格为1。
  5.网络被禁用；没有听力，设置了最大同行数量
     到0，发现被禁用。
`)

	case !ctx.IsSet(utils.NetworkIdFlag.Name):
		log.Info("Starting Geth on Ethereum mainnet...")
	}
	// 如果我们是Mainnet上的完整节点，则没有指定的-SCACHE，bump默认缓存津贴
	if ctx.String(utils.SyncModeFlag.Name) != "light" && !ctx.IsSet(utils.CacheFlag.Name) && !ctx.IsSet(utils.NetworkIdFlag.Name) {
		// Make sure we're not on any supported preconfigured testnet either
		if !ctx.IsSet(utils.RopstenFlag.Name) &&
			!ctx.IsSet(utils.SepoliaFlag.Name) &&
			!ctx.IsSet(utils.RinkebyFlag.Name) &&
			!ctx.IsSet(utils.GoerliFlag.Name) &&
			!ctx.IsSet(utils.KilnFlag.Name) &&
			!ctx.IsSet(utils.DeveloperFlag.Name) {
			// 不，我们真的在主网上。撞到缓存！
			log.Info("Bumping default cache on mainnet", "provided", ctx.Int(utils.CacheFlag.Name), "updated", 4096)
			ctx.Set(utils.CacheFlag.Name, strconv.Itoa(4096))
		}
	}
	// 如果我们在任何网络上运行一个轻端客户端，请将缓存放到一些有意义的低量
	if ctx.String(utils.SyncModeFlag.Name) == "light" && !ctx.IsSet(utils.CacheFlag.Name) {
		log.Info("Dropping default light client cache", "provided", ctx.Int(utils.CacheFlag.Name), "updated", 128)
		ctx.Set(utils.CacheFlag.Name, strconv.Itoa(128))
	}

	// 启动指标导出如果启用
	utils.SetupMetrics(ctx)

	// 启动系统运行时指标集合
	go metrics.CollectProcessMetrics(3 * time.Second)
}

// Geth是系统中的主要入口点，如果没有特殊的子命令。
//它基于命令行参数创建默认节点，并将其运行
//阻止模式，等待关闭它。
func geth(ctx *cli.Context) error {
	if args := ctx.Args().Slice(); len(args) > 0 {
		return fmt.Errorf("invalid command: %q", args[0])
	}

	prepare(ctx)
	// TODO：定义全节点对象
	stack, backend := makeFullNode(ctx)
	defer stack.Close()
	// TODO：启动全节点
	startNode(ctx, stack, backend, false)
	stack.Wait()
	return nil
}

// startnode启动系统节点和所有注册协议，之后
//它解锁任何请求的帐户，并启动RPC/IPC接口和
//矿工。

// 1、启动node；
// 2、解锁账户；
// 3、开启钱包事件监听；
func startNode(ctx *cli.Context, stack *node.Node, backend ethapi.Backend, isConsole bool) {
	debug.Memsize.Add("node", stack)

	// 启动节点本身
	utils.StartNode(ctx, stack, isConsole)

	// 解锁任何专门要求的帐户
	unlockAccounts(ctx, stack)

	// 注册钱包活动处理程序打开和自动衍生钱包
	events := make(chan accounts.WalletEvent, 16)
	stack.AccountManager().Subscribe(events)

	// 创建一个客户以与本地Geth节点进行交互。
	rpcClient, err := stack.Attach()
	if err != nil {
		utils.Fatalf("Failed to attach to self: %v", err)
	}
	ethClient := ethclient.NewClient(rpcClient)

	go func() {
		// Open any wallets already attached
		for _, wallet := range stack.AccountManager().Wallets() {
			if err := wallet.Open(""); err != nil {
				log.Warn("Failed to open wallet", "url", wallet.URL(), "err", err)
			}
		}
		// Listen for wallet event till termination
		for event := range events {
			switch event.Kind {
			case accounts.WalletArrived:
				if err := event.Wallet.Open(""); err != nil {
					log.Warn("New wallet appeared, failed to open", "url", event.Wallet.URL(), "err", err)
				}
			case accounts.WalletOpened:
				status, _ := event.Wallet.Status()
				log.Info("New wallet appeared", "url", event.Wallet.URL(), "status", status)

				var derivationPaths []accounts.DerivationPath
				if event.Wallet.URL().Scheme == "ledger" {
					derivationPaths = append(derivationPaths, accounts.LegacyLedgerBaseDerivationPath)
				}
				derivationPaths = append(derivationPaths, accounts.DefaultBaseDerivationPath)

				event.Wallet.SelfDerive(derivationPaths, ethClient)

			case accounts.WalletDropped:
				log.Info("Old wallet dropped", "url", event.Wallet.URL())
				event.Wallet.Close()
			}
		}
	}()

	// 产生一个独立的goroutine，以进行状态同步监控，
	//如果需要，请在同步完成时关闭节点。
	if ctx.Bool(utils.ExitWhenSyncedFlag.Name) {
		go func() {
			sub := stack.EventMux().Subscribe(downloader.DoneEvent{})
			defer sub.Unsubscribe()
			for {
				event := <-sub.Chan()
				if event == nil {
					continue
				}
				done, ok := event.Data.(downloader.DoneEvent)
				if !ok {
					continue
				}
				if timestamp := time.Unix(int64(done.Latest.Time), 0); time.Since(timestamp) < 10*time.Minute {
					log.Info("Synchronisation completed", "latestnum", done.Latest.Number, "latesthash", done.Latest.Hash(),
						"age", common.PrettyAge(timestamp))
					stack.Close()
				}
			}
		}()
	}

	// 启动辅助服务如果启用
	if ctx.Bool(utils.MiningEnabledFlag.Name) || ctx.Bool(utils.DeveloperFlag.Name) {
		// 只有在运行完整的以太坊节点时，采矿才有意义
		if ctx.String(utils.SyncModeFlag.Name) == "light" {
			utils.Fatalf("Light clients do not support mining")
		}
		ethBackend, ok := backend.(*eth.EthAPIBackend)
		if !ok {
			utils.Fatalf("Ethereum service not running")
		}
		//将汽油价格设置为CLI的极限并开始开采
		gasprice := flags.GlobalBig(ctx, utils.MinerGasPriceFlag.Name)
		ethBackend.TxPool().SetGasPrice(gasprice)
		// 开始采矿
		threads := ctx.Int(utils.MinerThreadsFlag.Name)
		if err := ethBackend.StartMining(threads); err != nil {
			utils.Fatalf("Failed to start mining: %v", err)
		}
	}
}

// 解锁解锁解锁任何特定要求的帐户。
func unlockAccounts(ctx *cli.Context, stack *node.Node) {
	var unlocks []string
	inputs := strings.Split(ctx.String(utils.UnlockedAccountFlag.Name), ",")
	for _, input := range inputs {
		if trimmed := strings.TrimSpace(input); trimmed != "" {
			unlocks = append(unlocks, trimmed)
		}
	}
	// 短路如果没有帐户要解锁。
	if len(unlocks) == 0 {
		return
	}
	// 如果如果节点的API暴露于外部，则不允许解锁不安全的帐户。
	//打印警告日志给用户并跳过解锁。
	if !stack.Config().InsecureUnlockAllowed && stack.Config().ExtRPCEnabled() {
		utils.Fatalf("Account unlock with HTTP access is forbidden!")
	}
	ks := stack.AccountManager().Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
	passwords := utils.MakePasswordList(ctx)
	for i, account := range unlocks {
		unlockAccount(ks, account, i, passwords)
	}
}
