//版权2020年作者
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
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

var (
	rpcRequestGauge        = metrics.NewRegisteredGauge("rpc/requests", nil)
	successfulRequestGauge = metrics.NewRegisteredGauge("rpc/success", nil)
	failedRequestGauge     = metrics.NewRegisteredGauge("rpc/failure", nil)

	// Servetime Histname是每次要求服务时间直方图的前缀。
	serveTimeHistName = "rpc/duration"

	rpcServingTimer = metrics.NewRegisteredTimer("rpc/duration/all", nil)
)

// 更新时间组织图跟踪远程RPC调用的服务时间。
func updateServeTimeHistogram(method string, success bool, elapsed time.Duration) {
	note := "success"
	if !success {
		note = "failure"
	}
	h := fmt.Sprintf("%s/%s/%s", serveTimeHistName, method, note)
	sampler := func() metrics.Sample {
		return metrics.ResettingSample(
			metrics.NewExpDecaySample(1028, 0.015),
		)
	}
	metrics.GetOrRegisterHistogramLazy(h, nil, sampler).Update(elapsed.Microseconds())
}
