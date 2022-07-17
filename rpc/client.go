//版权所有2016年作者
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

package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

var (
	ErrClientQuit                = errors.New("client is closed")
	ErrNoResult                  = errors.New("no result in JSON-RPC response")
	ErrSubscriptionQueueOverflow = errors.New("subscription queue overflow")
	errClientReconnected         = errors.New("client reconnected")
	errDead                      = errors.New("connection lost")
)

const (
	// Timeouts
	defaultDialTimeout = 10 * time.Second // 如果上下文没有截止日期，则使用
	subscribeTimeout   = 5 * time.Second  // 总体超时eth_subscribe，rpc_modules调用
)

const (
	//当订户无法跟上时，删除订阅。
	//
	//这可以通过提供足够尺寸的缓冲区的频道来解决这个问题，
	//但是，这在文档中可能会带来不便且难以解释。另一个问题
	//缓冲通道是缓冲区是静态的，即使可能不需要
	// 大多数时候。
	//
	//这里采用的方法是维护每订阅的链接列表缓冲区
	//按需收缩。如果缓冲区达到以下大小，则订阅为
	//掉落。
	maxClientSubscriptionBuffer = 20000
)

// Batchelem是批处理请求中的一个元素。
type BatchElem struct {
	Method string
	Args   []interface{}
	// 结果未列入该领域。结果必须设置为
	//所需类型的非nil指针值，否则响应将为
	//丢弃。
	Result interface{}
	// 如果服务器返回此请求的错误，则设置错误
	//将结果删除失败。它不是为I/O错误设置的。
	Error error
}

//客户端代表与RPC服务器的连接。
type Client struct {
	idgen    func() ID // for subscriptions
	isHTTP   bool      // connection type: http, ws or ipc
	services *serviceRegistry

	idCounter uint32

	// 当连接丢失时，该功能（如果非nil）被调用。
	reconnectFunc reconnectFunc

	// WriteConn用于写入呼叫者Goroutine上的连接。它应该
	//只有在派遣锁定的情况下才能在派遣外访问。写锁是
	//通过发送重新数字来拍摄并通过发送reqsent释放。
	writeConn jsonWriter

	// for dispatch
	close       chan struct{}
	closing     chan struct{}    // closed when client is quitting
	didClose    chan struct{}    // closed when client quits
	reconnected chan ServerCodec // where write/reconnect sends the new connection
	readOp      chan readOp      // read messages
	readErr     chan error       // errors from read
	reqInit     chan *requestOp  // register response IDs, takes write lock
	reqSent     chan error       // signals write completion, releases write lock
	reqTimeout  chan *requestOp  // removes response IDs when call timeout expires
}

type reconnectFunc func(ctx context.Context) (ServerCodec, error)

type clientContextKey struct{}

type clientConn struct {
	codec   ServerCodec
	handler *handler
}

func (c *Client) newClientConn(conn ServerCodec) *clientConn {
	ctx := context.Background()
	ctx = context.WithValue(ctx, clientContextKey{}, c)
	ctx = context.WithValue(ctx, peerInfoContextKey{}, conn.peerInfo())
	handler := newHandler(ctx, conn, c.idgen, c.services)
	return &clientConn{conn, handler}
}

func (cc *clientConn) close(err error, inflightReq *requestOp) {
	cc.handler.close(err, inflightReq)
	cc.codec.close()
}

type readOp struct {
	msgs  []*jsonrpcMessage
	batch bool
}

type requestOp struct {
	ids  []json.RawMessage
	err  error
	resp chan *jsonrpcMessage // receives up to len(ids) responses
	sub  *ClientSubscription  // only set for EthSubscribe requests
}

func (op *requestOp) wait(ctx context.Context, c *Client) (*jsonrpcMessage, error) {
	select {
	case <-ctx.Done():
		// Send the timeout to dispatch so it can remove the request IDs.
		if !c.isHTTP {
			select {
			case c.reqTimeout <- op:
			case <-c.closing:
			}
		}
		return nil, ctx.Err()
	case resp := <-op.resp:
		return resp, op.err
	}
}

//拨号为给定URL创建一个新客户端。
//
//当前支持的URL方案是“ HTTP”，“ HTTPS”，“ WS”和“ WSS”。如果Rawurl是一个
//没有URL方案的文件名，使用UNIX建立本地插座连接
//在受支持平台上的域插座，并在Windows上命名为管道。如果你想
//配置传输选项，改用DialHTTP，DialWebsocket或Dialipc。
//
//对于WebSocket连接，原点设置为本地主机名。
//
//如果连接丢失，客户端会自动重新连接。
func Dial(rawurl string) (*Client, error) {
	return DialContext(context.Background(), rawurl)
}

// DialContext与Dial一样创建一个新的RPC客户端。
//
//上下文用于取消或超时初始连接建立。确实如此
//不影响与客户的后续交互。
func DialContext(ctx context.Context, rawurl string) (*Client, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return DialHTTP(rawurl)
	case "ws", "wss":
		return DialWebsocket(ctx, rawurl, "")
	case "stdio":
		return DialStdIO(ctx)
	case "":
		return DialIPC(ctx, rawurl)
	default:
		return nil, fmt.Errorf("no known transport for URL scheme %q", u.Scheme)
	}
}

// 客户端FromContext从上下文（如果有）从上下文中检索客户端。这可以用来执行
//处理程序方法中的“反向调用”。
func ClientFromContext(ctx context.Context) (*Client, bool) {
	client, ok := ctx.Value(clientContextKey{}).(*Client)
	return client, ok
}

func newClient(initctx context.Context, connect reconnectFunc) (*Client, error) {
	conn, err := connect(initctx)
	if err != nil {
		return nil, err
	}
	c := initClient(conn, randomIDGenerator(), new(serviceRegistry))
	c.reconnectFunc = connect
	return c, nil
}

func initClient(conn ServerCodec, idgen func() ID, services *serviceRegistry) *Client {
	_, isHTTP := conn.(*httpConn)
	c := &Client{
		isHTTP:      isHTTP,
		idgen:       idgen,
		services:    services,
		writeConn:   conn,
		close:       make(chan struct{}),
		closing:     make(chan struct{}),
		didClose:    make(chan struct{}),
		reconnected: make(chan ServerCodec),
		readOp:      make(chan readOp),
		readErr:     make(chan error),
		reqInit:     make(chan *requestOp),
		reqSent:     make(chan error, 1),
		reqTimeout:  make(chan *requestOp),
	}
	if !isHTTP {
		go c.dispatch(conn)
	}
	return c
}

// registername根据给定名称为给定接收器类型创建服务。当没有
//给定接收器上的方法匹配标准为RPC方法或
//订阅返回错误。否则将创建新服务并添加到
//该客户端向服务器提供的服务集合。
func (c *Client) RegisterName(name string, receiver interface{}) error {
	return c.services.registerName(name, receiver)
}

func (c *Client) nextID() json.RawMessage {
	id := atomic.AddUint32(&c.idCounter, 1)
	return strconv.AppendUint(nil, uint64(id), 10)
}

// 支持modules调用rpc_modules方法，检索列表
//服务器上可用的API。
func (c *Client) SupportedModules() (map[string]string, error) {
	var result map[string]string
	ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
	defer cancel()
	err := c.CallContext(ctx, &result, "rpc_modules")
	return result, err
}

// 关闭关闭客户端，中止任何机上请求。
func (c *Client) Close() {
	if c.isHTTP {
		return
	}
	select {
	case c.close <- struct{}{}:
		<-c.didClose
	case <-c.didClose:
	}
}

// Setheader将自定义的HTTP标头添加到客户端的请求中。
//此方法仅适用于使用HTTP的客户端，它没有
//对客户使用另一台运输的任何效果。
func (c *Client) SetHeader(key, value string) {
	if !c.isHTTP {
		return
	}
	conn := c.writeConn.(*httpConn)
	conn.mu.Lock()
	conn.headers.Set(key, value)
	conn.mu.Unlock()
}

// 呼叫执行带有给定参数的JSON-RPC呼叫，并将其删除
//如果未发生错误，结果。
//
//结果必须是一个指针，以便软件包JSON可以将其删除。你
//也可以通过零，在这种情况下，结果将被忽略。
func (c *Client) Call(result interface{}, method string, args ...interface{}) error {
	ctx := context.Background()
	return c.CallContext(ctx, result, method, args...)
}

// CallContext用给定参数执行JSON-RPC调用。如果上下文是
//在通话成功返回之前已取消，CallContext立即返回。
//
//结果必须是一个指针，以便软件包JSON可以将其删除。你
//也可以通过零，在这种情况下，结果将被忽略。
func (c *Client) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	if result != nil && reflect.TypeOf(result).Kind() != reflect.Ptr {
		return fmt.Errorf("call result parameter must be pointer or nil interface: %v", result)
	}
	msg, err := c.newMessage(method, args...)
	if err != nil {
		return err
	}
	op := &requestOp{ids: []json.RawMessage{msg.ID}, resp: make(chan *jsonrpcMessage, 1)}

	if c.isHTTP {
		err = c.sendHTTP(ctx, op, msg)
	} else {
		err = c.send(ctx, op, msg)
	}
	if err != nil {
		return err
	}

	// dispatch has accepted the request and will close the channel when it quits.
	switch resp, err := op.wait(ctx, c); {
	case err != nil:
		return err
	case resp.Error != nil:
		return resp.Error
	case len(resp.Result) == 0:
		return ErrNoResult
	default:
		return json.Unmarshal(resp.Result, &result)
	}
}

// batchCall将所有给定的请求发送为单批次，并等待服务器
//返回所有响应。
//
//与呼叫相反，batchCall仅返回I/O错误。任何特定于
//通过相应batchelem的错误字段报告请求。
//
//请注意，批次调用可能不会在服务器端原子执行。
func (c *Client) BatchCall(b []BatchElem) error {
	ctx := context.Background()
	return c.BatchCallContext(ctx, b)
}

// batchcallcontext将所有给定的请求发送为单个批次，并等待服务器
//返回所有响应。等待时间由
//上下文的截止日期。
//
//与CallContext相反，BatchCallContext仅返回发生的错误
//发送请求时。任何特定于请求的错误均通过
//相应batchelem的错误字段。
//
//请注意，批次调用可能不会在服务器端原子执行。
func (c *Client) BatchCallContext(ctx context.Context, b []BatchElem) error {
	var (
		msgs = make([]*jsonrpcMessage, len(b))
		byID = make(map[string]int, len(b))
	)
	op := &requestOp{
		ids:  make([]json.RawMessage, len(b)),
		resp: make(chan *jsonrpcMessage, len(b)),
	}
	for i, elem := range b {
		msg, err := c.newMessage(elem.Method, elem.Args...)
		if err != nil {
			return err
		}
		msgs[i] = msg
		op.ids[i] = msg.ID
		byID[string(msg.ID)] = i
	}

	var err error
	if c.isHTTP {
		err = c.sendBatchHTTP(ctx, op, msgs)
	} else {
		err = c.send(ctx, op, msgs)
	}

	// Wait for all responses to come back.
	for n := 0; n < len(b) && err == nil; n++ {
		var resp *jsonrpcMessage
		resp, err = op.wait(ctx, c)
		if err != nil {
			break
		}
		// Find the element corresponding to this response.
		// The element is guaranteed to be present because dispatch
		// only sends valid IDs to our channel.
		elem := &b[byID[string(resp.ID)]]
		if resp.Error != nil {
			elem.Error = resp.Error
			continue
		}
		if len(resp.Result) == 0 {
			elem.Error = ErrNoResult
			continue
		}
		elem.Error = json.Unmarshal(resp.Result, elem.Result)
	}
	return err
}

// 通知发送通知，即不会期望响应的方法调用。
func (c *Client) Notify(ctx context.Context, method string, args ...interface{}) error {
	op := new(requestOp)
	msg, err := c.newMessage(method, args...)
	if err != nil {
		return err
	}
	msg.ID = nil

	if c.isHTTP {
		return c.sendHTTP(ctx, op, msg)
	}
	return c.send(ctx, op, msg)
}

// EthSubscriber在“ ETH”名称空间下注册订阅。
func (c *Client) EthSubscribe(ctx context.Context, channel interface{}, args ...interface{}) (*ClientSubscription, error) {
	return c.Subscribe(ctx, "eth", channel, args...)
}

// Shhsubscribe在“ SHH”名称空间下注册了订阅。
//弃用：使用订阅（ctx，“ shh”，...）。
func (c *Client) ShhSubscribe(ctx context.Context, channel interface{}, args ...interface{}) (*ClientSubscription, error) {
	return c.Subscribe(ctx, "shh", channel, args...)
}

// 订阅给定参数的“ <名称空间> _subscribe”方法，
//注册订阅。订阅的服务器通知是
//发送到给定的频道。通道的元素类型必须与
//订阅返回的内容的预期类型。
//
//上下文参数取消设置订阅但没有的RPC请求
//订阅返回后对订阅的影响。
//
//慢速订阅者最终将被删除。客户缓冲至20000年通知
//在考虑订户死亡之前。订阅错误通道将接收
// errsubscriptionqueueoverflow。在通道上使用足够大的缓冲区或确保
//该频道通常至少有一个读者来防止此问题。
func (c *Client) Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (*ClientSubscription, error) {
	// Check type of channel first.
	chanVal := reflect.ValueOf(channel)
	if chanVal.Kind() != reflect.Chan || chanVal.Type().ChanDir()&reflect.SendDir == 0 {
		panic(fmt.Sprintf("channel argument of Subscribe has type %T, need writable channel", channel))
	}
	if chanVal.IsNil() {
		panic("channel given to Subscribe must not be nil")
	}
	if c.isHTTP {
		return nil, ErrNotificationsUnsupported
	}

	msg, err := c.newMessage(namespace+subscribeMethodSuffix, args...)
	if err != nil {
		return nil, err
	}
	op := &requestOp{
		ids:  []json.RawMessage{msg.ID},
		resp: make(chan *jsonrpcMessage),
		sub:  newClientSubscription(c, namespace, chanVal),
	}

	// Send the subscription request.
	// The arrival and validity of the response is signaled on sub.quit.
	if err := c.send(ctx, op, msg); err != nil {
		return nil, err
	}
	if _, err := op.wait(ctx, c); err != nil {
		return nil, err
	}
	return op.sub, nil
}

func (c *Client) newMessage(method string, paramsIn ...interface{}) (*jsonrpcMessage, error) {
	msg := &jsonrpcMessage{Version: vsn, ID: c.nextID(), Method: method}
	if paramsIn != nil { // prevent sending "params":null
		var err error
		if msg.Params, err = json.Marshal(paramsIn); err != nil {
			return nil, err
		}
	}
	return msg, nil
}

// 将寄存器OP带有调度循环，然后在连接上发送味精。
//如果发送失败，则将OP进行检查。
func (c *Client) send(ctx context.Context, op *requestOp, msg interface{}) error {
	select {
	case c.reqInit <- op:
		err := c.write(ctx, msg, false)
		c.reqSent <- err
		return err
	case <-ctx.Done():
		// This can happen if the client is overloaded or unable to keep up with
		// subscription notifications.
		return ctx.Err()
	case <-c.closing:
		return ErrClientQuit
	}
}

func (c *Client) write(ctx context.Context, msg interface{}, retry bool) error {
	if c.writeConn == nil {
		// The previous write failed. Try to establish a new connection.
		if err := c.reconnect(ctx); err != nil {
			return err
		}
	}
	err := c.writeConn.writeJSON(ctx, msg)
	if err != nil {
		c.writeConn = nil
		if !retry {
			return c.write(ctx, msg, true)
		}
	}
	return err
}

func (c *Client) reconnect(ctx context.Context) error {
	if c.reconnectFunc == nil {
		return errDead
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, defaultDialTimeout)
		defer cancel()
	}
	newconn, err := c.reconnectFunc(ctx)
	if err != nil {
		log.Trace("RPC client reconnect failed", "err", err)
		return err
	}
	select {
	case c.reconnected <- newconn:
		c.writeConn = newconn
		return nil
	case <-c.didClose:
		newconn.close()
		return ErrClientQuit
	}
}

// 调度是客户端的主要循环。
//它将读取消息发送到等待通话和batchcall
//以及注册订阅的订阅通知。
func (c *Client) dispatch(codec ServerCodec) {
	var (
		lastOp      *requestOp  // tracks last send operation
		reqInitLock = c.reqInit // nil while the send lock is held
		conn        = c.newClientConn(codec)
		reading     = true
	)
	defer func() {
		close(c.closing)
		if reading {
			conn.close(ErrClientQuit, nil)
			c.drainRead()
		}
		close(c.didClose)
	}()

	// Spawn the initial read loop.
	go c.read(codec)

	for {
		select {
		case <-c.close:
			return

		// Read path:
		case op := <-c.readOp:
			if op.batch {
				conn.handler.handleBatch(op.msgs)
			} else {
				conn.handler.handleMsg(op.msgs[0])
			}

		case err := <-c.readErr:
			conn.handler.log.Debug("RPC connection read error", "err", err)
			conn.close(err, lastOp)
			reading = false

		// Reconnect:
		case newcodec := <-c.reconnected:
			log.Debug("RPC client reconnected", "reading", reading, "conn", newcodec.remoteAddr())
			if reading {
				// Wait for the previous read loop to exit. This is a rare case which
				// happens if this loop isn't notified in time after the connection breaks.
				// In those cases the caller will notice first and reconnect. Closing the
				// handler terminates all waiting requests (closing op.resp) except for
				// lastOp, which will be transferred to the new handler.
				conn.close(errClientReconnected, lastOp)
				c.drainRead()
			}
			go c.read(newcodec)
			reading = true
			conn = c.newClientConn(newcodec)
			// Re-register the in-flight request on the new handler
			// because that's where it will be sent.
			conn.handler.addRequestOp(lastOp)

		// Send path:
		case op := <-reqInitLock:
			// Stop listening for further requests until the current one has been sent.
			reqInitLock = nil
			lastOp = op
			conn.handler.addRequestOp(op)

		case err := <-c.reqSent:
			if err != nil {
				// Remove response handlers for the last send. When the read loop
				// goes down, it will signal all other current operations.
				conn.handler.removeRequestOp(lastOp)
			}
			// Let the next request in.
			reqInitLock = c.reqInit
			lastOp = nil

		case op := <-c.reqTimeout:
			conn.handler.removeRequestOp(op)
		}
	}
}

// 排水滴滴读取消息，直到发生错误。
func (c *Client) drainRead() {
	for {
		select {
		case <-c.readOp:
		case <-c.readErr:
			return
		}
	}
}

// read decodes RPC messages from a codec, feeding them into dispatch.
func (c *Client) read(codec ServerCodec) {
	for {
		msgs, batch, err := codec.readBatch()
		if _, ok := err.(*json.SyntaxError); ok {
			codec.writeJSON(context.Background(), errorMessage(&parseError{err.Error()}))
		}
		if err != nil {
			c.readErr <- err
			return
		}
		c.readOp <- readOp{msgs, batch}
	}
}
