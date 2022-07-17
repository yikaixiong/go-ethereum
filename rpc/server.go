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

package rpc

import (
	"context"
	"io"
	"sync/atomic"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/log"
)

const MetadataApi = "rpc"
const EngineApi = "engine"

// codecoption指定编解码器支持的消息类型。
//
//弃用：此选项不再由服务器兑现。
type CodecOption int

const (
	// OptionMethodInvocation is an indication that the codec supports RPC method calls
	OptionMethodInvocation CodecOption = 1 << iota

	// OptionSubscriptions is an indication that the codec supports RPC notifications
	OptionSubscriptions = 1 << iota // support pub sub
)

// 服务器是RPC服务器。
type Server struct {
	services serviceRegistry
	idgen    func() ID
	run      int32
	codecs   mapset.Set
}

// Newserver创建一个没有注册处理程序的新服务器实例。
func NewServer() *Server {
	server := &Server{idgen: randomIDGenerator(), codecs: mapset.NewSet(), run: 1}
	// Register the default service providing meta information about the RPC service such
	// as the services and methods it offers.
	rpcService := &RPCService{server}
	server.RegisterName(MetadataApi, rpcService)
	return server
}

// registername根据给定名称为给定接收器类型创建服务。当没有
//给定接收器上的方法匹配标准为RPC方法或
//订阅返回错误。否则将创建新服务并添加到
//该服务器向客户端提供的服务集合。
func (s *Server) RegisterName(name string, receiver interface{}) error {
	return s.services.registerName(name, receiver)
}

// Servecodec读取来自编解码器的传入请求，调用适当的回调并写入
//使用给定编解码器回复。它将阻止直到编解码器关闭或
//服务器已停止。无论哪种情况，编解码器都关闭。
//
//请注意，不再支持编解码器选项。
func (s *Server) ServeCodec(codec ServerCodec, options CodecOption) {
	defer codec.close()

	// Don't serve if server is stopped.
	if atomic.LoadInt32(&s.run) == 0 {
		return
	}

	// Add the codec to the set so it can be closed by Stop.
	s.codecs.Add(codec)
	defer s.codecs.Remove(codec)

	c := initClient(codec, s.idgen, &s.services)
	<-codec.closed()
	c.Close()
}

//ServesingLereQuest读取并处理给定编解码器的单个RPC请求。这个
//用于服务HTTP连接。不允许订阅和反向调用
//此模式。
func (s *Server) serveSingleRequest(ctx context.Context, codec ServerCodec) {
	// Don't serve if server is stopped.
	if atomic.LoadInt32(&s.run) == 0 {
		return
	}

	h := newHandler(ctx, codec, s.idgen, &s.services)
	h.allowSubscribe = false
	defer h.close(io.EOF, nil)

	reqs, batch, err := codec.readBatch()
	if err != nil {
		if err != io.EOF {
			codec.writeJSON(ctx, errorMessage(&invalidMessageError{"parse error"}))
		}
		return
	}
	if batch {
		h.handleBatch(reqs)
	} else {
		h.handleMsg(reqs[0])
	}
}

// 停止停止阅读新请求，等待停止请求超时以允许待处理
//要求完成的请求，然后关闭所有将取消待处理请求的编解码器和
//订阅。
func (s *Server) Stop() {
	if atomic.CompareAndSwapInt32(&s.run, 1, 0) {
		log.Debug("RPC server shutting down")
		s.codecs.Each(func(c interface{}) bool {
			c.(ServerCodec).close()
			return true
		})
	}
}

// RPCService提供有关服务器的元信息。
//例如提供有关加载模块的信息。
type RPCService struct {
	server *Server
}

// Modules returns the list of RPC services with their version number
func (s *RPCService) Modules() map[string]string {
	s.server.services.mu.Lock()
	defer s.server.services.mu.Unlock()

	modules := make(map[string]string)
	for name := range s.server.services.services {
		modules[name] = "1.0"
	}
	return modules
}
// Peerinfo包含有关网络连接远端的信息。
//
//这可以通过上下文中的RPC方法处理程序提供。称呼
// peerInfoFromContext获取有关客户连接的信息
//当前方法调用。
type PeerInfo struct {
	// Transport is name of the protocol used by the client.
	// This can be "http", "ws" or "ipc".
	Transport string

	// Address of client. This will usually contain the IP address and port.
	RemoteAddr string

	// Addditional information for HTTP and WebSocket connections.
	HTTP struct {
		// Protocol version, i.e. "HTTP/1.1". This is not set for WebSocket.
		Version string
		// Header values sent by the client.
		UserAgent string
		Origin    string
		Host      string
	}
}

type peerInfoContextKey struct{}

// PeerInfoFromContext返回有关客户端网络连接的信息。
//将其与传递给RPC方法处理程序功能的上下文一起使用。
//
//如果CTX中没有连接信息，则返回零值。
func PeerInfoFromContext(ctx context.Context) PeerInfo {
	info, _ := ctx.Value(peerInfoContextKey{}).(PeerInfo)
	return info
}
