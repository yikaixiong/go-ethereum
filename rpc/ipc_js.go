//版权2018 go-ethereum作者
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

// GO：构建JS
// +构建JS

package rpc

import (
	"context"
	"errors"
	"net"
)

var errNotSupported = errors.New("rpc: not supported")

// Ipclisten将在给定端点上创建一个命名的管道。
func ipcListen(endpoint string) (net.Listener, error) {
	return nil, errNotSupported
}

// newipcconnection将连接到具有给定端点名称的指定管道。
func newIPCConnection(ctx context.Context, endpoint string) (net.Conn, error) {
	return nil, errNotSupported
}
