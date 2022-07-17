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

package node

import (
	crand "crypto/rand"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/prometheus/tsdb/fileutil"
)

// Node is a container on which services can be registered.
type Node struct {
	eventmux      *event.TypeMux
	config        *Config
	accman        *accounts.Manager
	log           log.Logger
	keyDir        string            // key store directory
	keyDirTemp    bool              // If true, key directory will be removed by Stop
	dirLock       fileutil.Releaser // prevents concurrent use of instance directory
	stop          chan struct{}     // Channel to wait for termination notifications
	server        *p2p.Server       // Currently running P2P networking layer
	startStopLock sync.Mutex        // Start/Stop are protected by an additional lock
	state         int               // Tracks state of node lifecycle

	lock          sync.Mutex
	lifecycles    []Lifecycle // All registered backends, services, and auxiliary services that have a lifecycle
	rpcAPIs       []rpc.API   // List of APIs currently provided by the node
	http          *httpServer //
	ws            *httpServer //
	httpAuth      *httpServer //
	wsAuth        *httpServer //
	ipc           *ipcServer  // Stores information about the ipc http server
	inprocHandler *rpc.Server // In-process RPC request handler to process the API requests

	databases map[*closeTrackingDB]struct{} // All open databases
}

const (
	initializingState = iota
	runningState
	closedState
)

// New创建了一个新的P2P节点，可以进行协议注册。
func New(conf *Config) (*Node, error) {
	// 复制配置并解析datadir，以便将来更改为当前
	//工作目录不影响节点。
	confCopy := *conf
	conf = &confCopy
	if conf.DataDir != "" {
		absdatadir, err := filepath.Abs(conf.DataDir)
		if err != nil {
			return nil, err
		}
		conf.DataDir = absdatadir
	}
	if conf.Logger == nil {
		conf.Logger = log.New()
	}

	// 确保实例名称不会引起与奇怪的冲突
	//数据目录中的其他文件。
	if strings.ContainsAny(conf.Name, `/\`) {
		return nil, errors.New(`Config.Name must not contain '/' or '\'`)
	}
	if conf.Name == datadirDefaultKeyStore {
		return nil, errors.New(`Config.Name cannot be "` + datadirDefaultKeyStore + `"`)
	}
	if strings.HasSuffix(conf.Name, ".ipc") {
		return nil, errors.New(`Config.Name cannot end in ".ipc"`)
	}

	node := &Node{
		config:        conf,
		inprocHandler: rpc.NewServer(),
		eventmux:      new(event.TypeMux),
		log:           conf.Logger,
		stop:          make(chan struct{}),
		server:        &p2p.Server{Config: conf.P2P},
		databases:     make(map[*closeTrackingDB]struct{}),
	}

	// Register built-in APIs.
	node.rpcAPIs = append(node.rpcAPIs, node.apis()...)

	// Acquire the instance directory lock.
	if err := node.openDataDir(); err != nil {
		return nil, err
	}
	keyDir, isEphem, err := getKeyStoreDir(conf)
	if err != nil {
		return nil, err
	}
	node.keyDir = keyDir
	node.keyDirTemp = isEphem
	// Creates an empty AccountManager with no backends. Callers (e.g. cmd/geth)
	// are required to add the backends later on.
	node.accman = accounts.NewManager(&accounts.Config{InsecureUnlockAllowed: conf.InsecureUnlockAllowed})

	// Initialize the p2p server. This creates the node key and discovery databases.
	node.server.Config.PrivateKey = node.config.NodeKey()
	node.server.Config.Name = node.config.NodeName()
	node.server.Config.Logger = node.log
	if node.server.Config.StaticNodes == nil {
		node.server.Config.StaticNodes = node.config.StaticNodes()
	}
	if node.server.Config.TrustedNodes == nil {
		node.server.Config.TrustedNodes = node.config.TrustedNodes()
	}
	if node.server.Config.NodeDatabase == "" {
		node.server.Config.NodeDatabase = node.config.NodeDB()
	}

	// Check HTTP/WS prefixes are valid.
	if err := validatePrefix("HTTP", conf.HTTPPathPrefix); err != nil {
		return nil, err
	}
	if err := validatePrefix("WebSocket", conf.WSPathPrefix); err != nil {
		return nil, err
	}

	// Configure RPC servers.
	node.http = newHTTPServer(node.log, conf.HTTPTimeouts)
	node.httpAuth = newHTTPServer(node.log, conf.HTTPTimeouts)
	node.ws = newHTTPServer(node.log, rpc.DefaultHTTPTimeouts)
	node.wsAuth = newHTTPServer(node.log, rpc.DefaultHTTPTimeouts)
	node.ipc = newIPCServer(node.log, conf.IPCEndpoint())

	return node, nil
}

//开始所有注册的生命周期，RPC服务和P2P网络。
//节点只能启动一次。
// TODO: 启动节点
func (n *Node) Start() error {
	n.startStopLock.Lock()
	defer n.startStopLock.Unlock()

	n.lock.Lock()
	switch n.state {
	case runningState:
		n.lock.Unlock()
		return ErrNodeRunning
	case closedState:
		n.lock.Unlock()
		return ErrNodeStopped
	}
	n.state = runningState
	// 打开网络和RPC终点
	err := n.openEndpoints()
	lifecycles := make([]Lifecycle, len(n.lifecycles))
	copy(lifecycles, n.lifecycles)
	n.lock.Unlock()

	// 检查端点启动是否失败。
	if err != nil {
		n.doClose(nil)
		return err
	}
	// 启动所有注册的生命周期。
	var started []Lifecycle
	for _, lifecycle := range lifecycles {
		if err = lifecycle.Start(); err != nil {
			break
		}
		started = append(started, lifecycle)
	}
	// 检查任何生命周期是否无法启动。
	if err != nil {
		n.stopServices(started)
		n.doClose(nil)
	}
	return err
}

//关闭停止节点并释放获得的资源
//节点构造函数新。
func (n *Node) Close() error {
	n.startStopLock.Lock()
	defer n.startStopLock.Unlock()

	n.lock.Lock()
	state := n.state
	n.lock.Unlock()
	switch state {
	case initializingState:
		// The node was never started.
		return n.doClose(nil)
	case runningState:
		// The node was started, release resources acquired by Start().
		var errs []error
		if err := n.stopServices(n.lifecycles); err != nil {
			errs = append(errs, err)
		}
		return n.doClose(errs)
	case closedState:
		return ErrNodeStopped
	default:
		panic(fmt.Sprintf("node is in unknown state %d", state))
	}
}

// Doclose释放了新（）获得的资源，收集错误。
func (n *Node) doClose(errs []error) error {
	// 关闭数据库。这需要锁，因为它需要
//与opendatabase*同步。
	n.lock.Lock()
	n.state = closedState
	errs = append(errs, n.closeDatabases()...)
	n.lock.Unlock()

	if err := n.accman.Close(); err != nil {
		errs = append(errs, err)
	}
	if n.keyDirTemp {
		if err := os.RemoveAll(n.keyDir); err != nil {
			errs = append(errs, err)
		}
	}

	// Release instance directory lock.
	n.closeDataDir()

	// Unblock n.Wait.
	close(n.stop)

	// Report any errors that might have occurred.
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("%v", errs)
	}
}

// OpenEndPoints启动所有网络和RPC端点。
func (n *Node) openEndpoints() error {
	// start networking endpoints
	n.log.Info("Starting peer-to-peer node", "instance", n.server.Name)
	if err := n.server.Start(); err != nil {
		return convertFileLockError(err)
	}
	// start RPC endpoints
	err := n.startRPC()
	if err != nil {
		n.stopRPC()
		n.server.Stop()
	}
	return err
}

// containsLifeCycle检查“ LFS”是否包含“ L”。
func containsLifecycle(lfs []Lifecycle, l Lifecycle) bool {
	for _, obj := range lfs {
		if obj == l {
			return true
		}
	}
	return false
}

// StopServices终止运行服务，RPC和P2P网络。
//这是起点的倒数。
func (n *Node) stopServices(running []Lifecycle) error {
	n.stopRPC()

	// Stop running lifecycles in reverse order.
	failure := &StopError{Services: make(map[reflect.Type]error)}
	for i := len(running) - 1; i >= 0; i-- {
		if err := running[i].Stop(); err != nil {
			failure.Services[reflect.TypeOf(running[i])] = err
		}
	}

	// Stop p2p networking.
	n.server.Stop()

	if len(failure.Services) > 0 {
		return failure
	}
	return nil
}

func (n *Node) openDataDir() error {
	if n.config.DataDir == "" {
		return nil // ephemeral
	}

	instdir := filepath.Join(n.config.DataDir, n.config.name())
	if err := os.MkdirAll(instdir, 0700); err != nil {
		return err
	}
	// Lock the instance directory to prevent concurrent use by another instance as well as
	// accidental use of the instance directory as a database.
	release, _, err := fileutil.Flock(filepath.Join(instdir, "LOCK"))
	if err != nil {
		return convertFileLockError(err)
	}
	n.dirLock = release
	return nil
}

func (n *Node) closeDataDir() {
	// Release instance directory lock.
	if n.dirLock != nil {
		if err := n.dirLock.Release(); err != nil {
			n.log.Error("Can't release datadir lock", "err", err)
		}
		n.dirLock = nil
	}
}

//获取JWTSecret加载JWT-secret，要么从提供的配置，
//或默认位置。如果两个都不存在，它会生成
//一个新的秘密，并存储默认位置。
func (n *Node) obtainJWTSecret(cliParam string) ([]byte, error) {
	fileName := cliParam
	if len(fileName) == 0 {
		// no path provided, use default
		fileName = n.ResolvePath(datadirJWTKey)
	}
	// try reading from file
	if data, err := os.ReadFile(fileName); err == nil {
		jwtSecret := common.FromHex(strings.TrimSpace(string(data)))
		if len(jwtSecret) == 32 {
			log.Info("Loaded JWT secret file", "path", fileName, "crc32", fmt.Sprintf("%#x", crc32.ChecksumIEEE(jwtSecret)))
			return jwtSecret, nil
		}
		log.Error("Invalid JWT secret", "path", fileName, "length", len(jwtSecret))
		return nil, errors.New("invalid JWT secret")
	}
	// Need to generate one
	jwtSecret := make([]byte, 32)
	crand.Read(jwtSecret)
	// if we're in --dev mode, don't bother saving, just show it
	if fileName == "" {
		log.Info("Generated ephemeral JWT secret", "secret", hexutil.Encode(jwtSecret))
		return jwtSecret, nil
	}
	if err := os.WriteFile(fileName, []byte(hexutil.Encode(jwtSecret)), 0600); err != nil {
		return nil, err
	}
	log.Info("Generated JWT secret", "path", fileName)
	return jwtSecret, nil
}

// startrpc是一种辅助方法，用于在节点期间配置所有各种RPC端点
// 启动。这并不是要在此后的任何时候被调用，因为它可以确定
//关于节点状态的假设。
func (n *Node) startRPC() error {
	if err := n.startInProc(); err != nil {
		return err
	}

	// Configure IPC.
	if n.ipc.endpoint != "" {
		if err := n.ipc.start(n.rpcAPIs); err != nil {
			return err
		}
	}
	var (
		servers   []*httpServer
		open, all = n.GetAPIs()
	)

	initHttp := func(server *httpServer, apis []rpc.API, port int) error {
		if err := server.setListenAddr(n.config.HTTPHost, port); err != nil {
			return err
		}
		if err := server.enableRPC(apis, httpConfig{
			CorsAllowedOrigins: n.config.HTTPCors,
			Vhosts:             n.config.HTTPVirtualHosts,
			Modules:            n.config.HTTPModules,
			prefix:             n.config.HTTPPathPrefix,
		}); err != nil {
			return err
		}
		servers = append(servers, server)
		return nil
	}

	initWS := func(apis []rpc.API, port int) error {
		server := n.wsServerForPort(port, false)
		if err := server.setListenAddr(n.config.WSHost, port); err != nil {
			return err
		}
		if err := server.enableWS(n.rpcAPIs, wsConfig{
			Modules: n.config.WSModules,
			Origins: n.config.WSOrigins,
			prefix:  n.config.WSPathPrefix,
		}); err != nil {
			return err
		}
		servers = append(servers, server)
		return nil
	}

	initAuth := func(apis []rpc.API, port int, secret []byte) error {
		// Enable auth via HTTP
		server := n.httpAuth
		if err := server.setListenAddr(n.config.AuthAddr, port); err != nil {
			return err
		}
		if err := server.enableRPC(apis, httpConfig{
			CorsAllowedOrigins: DefaultAuthCors,
			Vhosts:             n.config.AuthVirtualHosts,
			Modules:            DefaultAuthModules,
			prefix:             DefaultAuthPrefix,
			jwtSecret:          secret,
		}); err != nil {
			return err
		}
		servers = append(servers, server)
		// Enable auth via WS
		server = n.wsServerForPort(port, true)
		if err := server.setListenAddr(n.config.AuthAddr, port); err != nil {
			return err
		}
		if err := server.enableWS(apis, wsConfig{
			Modules:   DefaultAuthModules,
			Origins:   DefaultAuthOrigins,
			prefix:    DefaultAuthPrefix,
			jwtSecret: secret,
		}); err != nil {
			return err
		}
		servers = append(servers, server)
		return nil
	}

	// Set up HTTP.
	if n.config.HTTPHost != "" {
		// Configure legacy unauthenticated HTTP.
		if err := initHttp(n.http, open, n.config.HTTPPort); err != nil {
			return err
		}
	}
	// Configure WebSocket.
	if n.config.WSHost != "" {
		// legacy unauthenticated
		if err := initWS(open, n.config.WSPort); err != nil {
			return err
		}
	}
	// Configure authenticated API
	if len(open) != len(all) {
		jwtSecret, err := n.obtainJWTSecret(n.config.JWTSecret)
		if err != nil {
			return err
		}
		if err := initAuth(all, n.config.AuthPort, jwtSecret); err != nil {
			return err
		}
	}
	// Start the servers
	for _, server := range servers {
		if err := server.start(); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) wsServerForPort(port int, authenticated bool) *httpServer {
	httpServer, wsServer := n.http, n.ws
	if authenticated {
		httpServer, wsServer = n.httpAuth, n.wsAuth
	}
	if n.config.HTTPHost == "" || httpServer.port == port {
		return httpServer
	}
	return wsServer
}

func (n *Node) stopRPC() {
	n.http.stop()
	n.ws.stop()
	n.httpAuth.stop()
	n.wsAuth.stop()
	n.ipc.stop()
	n.stopInProc()
}

// StartInProc在InProc服务器上注册所有RPC API。
func (n *Node) startInProc() error {
	for _, api := range n.rpcAPIs {
		if err := n.inprocHandler.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}
	return nil
}

// StopInProc终止了过程中的RPC端点。
func (n *Node) stopInProc() {
	n.inprocHandler.Stop()
}

// Wait blocks until the node is closed.
func (n *Node) Wait() {
	<-n.stop
}

// registerLifeCycle在节点上注册给定的生命周期。
func (n *Node) RegisterLifecycle(lifecycle Lifecycle) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state != initializingState {
		panic("can't register lifecycle on running/stopped node")
	}
	if containsLifecycle(n.lifecycles, lifecycle) {
		panic(fmt.Sprintf("attempt to register lifecycle %T more than once", lifecycle))
	}
	n.lifecycles = append(n.lifecycles, lifecycle)
}

// registerProtocols将Backend的协议添加到节点的P2P服务器中。
func (n *Node) RegisterProtocols(protocols []p2p.Protocol) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state != initializingState {
		panic("can't register protocols on running/stopped node")
	}
	n.server.Protocols = append(n.server.Protocols, protocols...)
}

// 寄存器登录节点上提供的API。
func (n *Node) RegisterAPIs(apis []rpc.API) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state != initializingState {
		panic("can't register APIs on running/stopped node")
	}
	n.rpcAPIs = append(n.rpcAPIs, apis...)
}

// Getapis返回两组API，这两个API都不需要
//身份验证和完整集
func (n *Node) GetAPIs() (unauthenticated, all []rpc.API) {
	for _, api := range n.rpcAPIs {
		if !api.Authenticated {
			unauthenticated = append(unauthenticated, api)
		}
	}
	return unauthenticated, n.rpcAPIs
}

// registerHandler将处理程序安装在规范HTTP服务器上的给定路径上。
//
// HTTP服务器启动时，处理程序的名称显示在日志消息中
//，应该是处理程序提供的服务的描述性术语。
func (n *Node) RegisterHandler(name, path string, handler http.Handler) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state != initializingState {
		panic("can't register HTTP handler on running/stopped node")
	}

	n.http.mux.Handle(path, handler)
	n.http.handlerNames[path] = name
}

// 附件创建附加在程序内API处理程序上的RPC客户端。
func (n *Node) Attach() (*rpc.Client, error) {
	return rpc.DialInProc(n.inprocHandler), nil
}

// RPCHandler returns the in-process RPC request handler.
func (n *Node) RPCHandler() (*rpc.Server, error) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.state == closedState {
		return nil, ErrNodeStopped
	}
	return n.inprocHandler, nil
}

// Config returns the configuration of node.
func (n *Node) Config() *Config {
	return n.config
}

//服务器检索当前运行的P2P网络层。这种方法是指
//仅检查当前运行服务器的字段。呼叫者不应该
//启动或停止返回的服务器。
func (n *Node) Server() *p2p.Server {
	n.lock.Lock()
	defer n.lock.Unlock()

	return n.server
}

// DataDir retrieves the current datadir used by the protocol stack.
// Deprecated: No files should be stored in this directory, use InstanceDir instead.
func (n *Node) DataDir() string {
	return n.config.DataDir
}

// InstanceDir retrieves the instance directory used by the protocol stack.
func (n *Node) InstanceDir() string {
	return n.config.instanceDir()
}

// KeyStoreDir retrieves the key directory
func (n *Node) KeyStoreDir() string {
	return n.keyDir
}

// AccountManager retrieves the account manager used by the protocol stack.
func (n *Node) AccountManager() *accounts.Manager {
	return n.accman
}

// IPCEndpoint retrieves the current IPC endpoint used by the protocol stack.
func (n *Node) IPCEndpoint() string {
	return n.ipc.endpoint
}

// httpendpoint返回HTTP服务器的URL。请注意，此URL没有
//包含HTTPPATHPREFIX设置的JSON-RPC路径前缀。
func (n *Node) HTTPEndpoint() string {
	return "http://" + n.http.listenAddr()
}

// WSEndpoint returns the current JSON-RPC over WebSocket endpoint.
func (n *Node) WSEndpoint() string {
	if n.http.wsAllowed() {
		return "ws://" + n.http.listenAddr() + n.http.wsConfig.prefix
	}
	return "ws://" + n.ws.listenAddr() + n.ws.wsConfig.prefix
}

// EventMux retrieves the event multiplexer used by all the network services in
// the current protocol stack.
func (n *Node) EventMux() *event.TypeMux {
	return n.eventmux
}

// opendatabase打开具有给定名称的现有数据库（或者如果没有，则创建一个数据库
//可以从节点的实例目录中找到上一个。如果节点为
//短暂的，返回存储器数据库。
func (n *Node) OpenDatabase(name string, cache, handles int, namespace string, readonly bool) (ethdb.Database, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.state == closedState {
		return nil, ErrNodeStopped
	}

	var db ethdb.Database
	var err error
	if n.config.DataDir == "" {
		db = rawdb.NewMemoryDatabase()
	} else {
		db, err = rawdb.NewLevelDBDatabase(n.ResolvePath(name), cache, handles, namespace, readonly)
	}

	if err == nil {
		db = n.wrapDatabase(db)
	}
	return db, err
}
// opendatabasewithfreezer打开具有给定名称的现有数据库（或
//如果在节点的数据目录中找不到以前的发现，则创建一个
//还将链式冰柜附加到它，从而将古代链数据从
//数据库到不可变的附加文件。如果节点是短暂的，则
//返回内存数据库。
// TODO: 打开数据库，返回数据库对象
func (n *Node) OpenDatabaseWithFreezer(name string, cache, handles int, freezer, namespace string, readonly bool) (ethdb.Database, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.state == closedState {
		return nil, ErrNodeStopped
	}

	var db ethdb.Database
	var err error
	if n.config.DataDir == "" {
		db = rawdb.NewMemoryDatabase()
	} else {
		root := n.ResolvePath(name)
		switch {
		case freezer == "":
			freezer = filepath.Join(root, "ancient")
		case !filepath.IsAbs(freezer):
			freezer = n.ResolvePath(freezer)
		}
		db, err = rawdb.NewLevelDBDatabaseWithFreezer(root, cache, handles, freezer, namespace, readonly)
	}

	if err == nil {
		db = n.wrapDatabase(db)
	}
	return db, err
}

// ResolvePath returns the absolute path of a resource in the instance directory.
func (n *Node) ResolvePath(x string) string {
	return n.config.ResolvePath(x)
}

// closeTrackingDB wraps the Close method of a database. When the database is closed by the
// service, the wrapper removes it from the node's database map. This ensures that Node
// won't auto-close the database if it is closed by the service that opened it.
type closeTrackingDB struct {
	ethdb.Database
	n *Node
}

func (db *closeTrackingDB) Close() error {
	db.n.lock.Lock()
	delete(db.n.databases, db)
	db.n.lock.Unlock()
	return db.Database.Close()
}

// wrapDatabase ensures the database will be auto-closed when Node is closed.
func (n *Node) wrapDatabase(db ethdb.Database) ethdb.Database {
	wrapper := &closeTrackingDB{db, n}
	n.databases[wrapper] = struct{}{}
	return wrapper
}

// closeDatabases closes all open databases.
func (n *Node) closeDatabases() (errors []error) {
	for db := range n.databases {
		delete(n.databases, db)
		if err := db.Database.Close(); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}
