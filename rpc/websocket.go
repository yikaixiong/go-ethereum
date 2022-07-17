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
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/log"
	"github.com/gorilla/websocket"
)

const (
	wsReadBuffer       = 1024
	wsWriteBuffer      = 1024
	wsPingInterval     = 60 * time.Second
	wsPingWriteTimeout = 5 * time.Second
	wsPongTimeout      = 30 * time.Second
	wsMessageSizeLimit = 15 * 1024 * 1024
)

var wsBufferPool = new(sync.Pool)

// WebSockethandler返回一个将JSON-RPC服务的处理程序返回Websocket连接。
//
//允许的人应为允许原点URL的逗号分隔列表。
//要允许与任何原点的连接，请通过“*”。
func (s *Server) WebsocketHandler(allowedOrigins []string) http.Handler {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  wsReadBuffer,
		WriteBufferSize: wsWriteBuffer,
		WriteBufferPool: wsBufferPool,
		CheckOrigin:     wsHandshakeValidator(allowedOrigins),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Debug("WebSocket upgrade failed", "err", err)
			return
		}
		codec := newWebsocketCodec(conn, r.Host, r.Header)
		s.ServeCodec(codec, 0)
	})
}

// wshandshakevalidator返回一个处理程序，该处理程序验证了在
// WebSocket升级过程。当一个'*'指定为允许的起源时
//连接被接受。
func wsHandshakeValidator(allowedOrigins []string) func(*http.Request) bool {
	origins := mapset.NewSet()
	allowAllOrigins := false

	for _, origin := range allowedOrigins {
		if origin == "*" {
			allowAllOrigins = true
		}
		if origin != "" {
			origins.Add(origin)
		}
	}
	// allow localhost if no allowedOrigins are specified.
	if len(origins.ToSlice()) == 0 {
		origins.Add("http://localhost")
		if hostname, err := os.Hostname(); err == nil {
			origins.Add("http://" + hostname)
		}
	}
	log.Debug(fmt.Sprintf("Allowed origin(s) for WS RPC interface %v", origins.ToSlice()))

	f := func(req *http.Request) bool {
		//跳过原点验证，如果没有原点标头。原点检查
//应该预防基于浏览器的攻击。浏览器始终设置
// 起源。非浏览器软件可以将任何内容都放在原始
//提供其他安全性。
		if _, ok := req.Header["Origin"]; !ok {
			return true
		}
		// Verify origin against allow list.
		origin := strings.ToLower(req.Header.Get("Origin"))
		if allowAllOrigins || originIsAllowed(origins, origin) {
			return true
		}
		log.Warn("Rejected WebSocket connection", "origin", origin)
		return false
	}

	return f
}

type wsHandshakeError struct {
	err    error
	status string
}

func (e wsHandshakeError) Error() string {
	s := e.err.Error()
	if e.status != "" {
		s += " (HTTP status " + e.status + ")"
	}
	return s
}

func originIsAllowed(allowedOrigins mapset.Set, browserOrigin string) bool {
	it := allowedOrigins.Iterator()
	for origin := range it.C {
		if ruleAllowsOrigin(origin.(string), browserOrigin) {
			return true
		}
	}
	return false
}

func ruleAllowsOrigin(allowedOrigin string, browserOrigin string) bool {
	var (
		allowedScheme, allowedHostname, allowedPort string
		browserScheme, browserHostname, browserPort string
		err                                         error
	)
	allowedScheme, allowedHostname, allowedPort, err = parseOriginURL(allowedOrigin)
	if err != nil {
		log.Warn("Error parsing allowed origin specification", "spec", allowedOrigin, "error", err)
		return false
	}
	browserScheme, browserHostname, browserPort, err = parseOriginURL(browserOrigin)
	if err != nil {
		log.Warn("Error parsing browser 'Origin' field", "Origin", browserOrigin, "error", err)
		return false
	}
	if allowedScheme != "" && allowedScheme != browserScheme {
		return false
	}
	if allowedHostname != "" && allowedHostname != browserHostname {
		return false
	}
	if allowedPort != "" && allowedPort != browserPort {
		return false
	}
	return true
}

func parseOriginURL(origin string) (string, string, string, error) {
	parsedURL, err := url.Parse(strings.ToLower(origin))
	if err != nil {
		return "", "", "", err
	}
	var scheme, hostname, port string
	if strings.Contains(origin, "://") {
		scheme = parsedURL.Scheme
		hostname = parsedURL.Hostname()
		port = parsedURL.Port()
	} else {
		scheme = ""
		hostname = parsedURL.Scheme
		port = parsedURL.Opaque
		if hostname == "" {
			hostname = origin
		}
	}
	return scheme, hostname, port, nil
}

// DialWebsocketWithDialer创建了一个新的RPC客户端，该客户端与JSON RPC服务器通信
//使用提供的拨号器在给定端点上侦听。
func DialWebsocketWithDialer(ctx context.Context, endpoint, origin string, dialer websocket.Dialer) (*Client, error) {
	endpoint, header, err := wsClientHeaders(endpoint, origin)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, func(ctx context.Context) (ServerCodec, error) {
		conn, resp, err := dialer.DialContext(ctx, endpoint, header)
		if err != nil {
			hErr := wsHandshakeError{err: err}
			if resp != nil {
				hErr.status = resp.Status
			}
			return nil, hErr
		}
		return newWebsocketCodec(conn, endpoint, header), nil
	})
}

// DialWebsocket创建一个新的RPC客户端，该客户端与JSON RPC服务器通信
//在给定端点上侦听。
//
//上下文用于初始连接建立。它不是
//影响与客户的后续交互。
func DialWebsocket(ctx context.Context, endpoint, origin string) (*Client, error) {
	dialer := websocket.Dialer{
		ReadBufferSize:  wsReadBuffer,
		WriteBufferSize: wsWriteBuffer,
		WriteBufferPool: wsBufferPool,
	}
	return DialWebsocketWithDialer(ctx, endpoint, origin, dialer)
}

func wsClientHeaders(endpoint, origin string) (string, http.Header, error) {
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return endpoint, nil, err
	}
	header := make(http.Header)
	if origin != "" {
		header.Add("origin", origin)
	}
	if endpointURL.User != nil {
		b64auth := base64.StdEncoding.EncodeToString([]byte(endpointURL.User.String()))
		header.Add("authorization", "Basic "+b64auth)
		endpointURL.User = nil
	}
	return endpointURL.String(), header, nil
}

type websocketCodec struct {
	*jsonCodec
	conn *websocket.Conn
	info PeerInfo

	wg        sync.WaitGroup
	pingReset chan struct{}
}

func newWebsocketCodec(conn *websocket.Conn, host string, req http.Header) ServerCodec {
	conn.SetReadLimit(wsMessageSizeLimit)
	conn.SetPongHandler(func(appData string) error {
		conn.SetReadDeadline(time.Time{})
		return nil
	})
	wc := &websocketCodec{
		jsonCodec: NewFuncCodec(conn, conn.WriteJSON, conn.ReadJSON).(*jsonCodec),
		conn:      conn,
		pingReset: make(chan struct{}, 1),
		info: PeerInfo{
			Transport:  "ws",
			RemoteAddr: conn.RemoteAddr().String(),
		},
	}
	// Fill in connection details.
	wc.info.HTTP.Host = host
	wc.info.HTTP.Origin = req.Get("Origin")
	wc.info.HTTP.UserAgent = req.Get("User-Agent")
	// Start pinger.
	wc.wg.Add(1)
	go wc.pingLoop()
	return wc
}

func (wc *websocketCodec) close() {
	wc.jsonCodec.close()
	wc.wg.Wait()
}

func (wc *websocketCodec) peerInfo() PeerInfo {
	return wc.info
}

func (wc *websocketCodec) writeJSON(ctx context.Context, v interface{}) error {
	err := wc.jsonCodec.writeJSON(ctx, v)
	if err == nil {
		// Notify pingLoop to delay the next idle ping.
		select {
		case wc.pingReset <- struct{}{}:
		default:
		}
	}
	return err
}

// pingLoop sends periodic ping frames when the connection is idle.
func (wc *websocketCodec) pingLoop() {
	var timer = time.NewTimer(wsPingInterval)
	defer wc.wg.Done()
	defer timer.Stop()

	for {
		select {
		case <-wc.closed():
			return
		case <-wc.pingReset:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(wsPingInterval)
		case <-timer.C:
			wc.jsonCodec.encMu.Lock()
			wc.conn.SetWriteDeadline(time.Now().Add(wsPingWriteTimeout))
			wc.conn.WriteMessage(websocket.PingMessage, nil)
			wc.conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
			wc.jsonCodec.encMu.Unlock()
			timer.Reset(wsPingInterval)
		}
	}
}
