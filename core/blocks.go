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

package core

import "github.com/ethereum/go-ethereum/common"

// BadHashes represent a set of manually tracked bad hashes (usually hard forks)
var BadHashes = map[common.Hash]bool{
	common.HexToHash("05bef30ef572270f654746da22639a7a0c97dd97a7050b9e252391996aaeb689"): true,
	common.HexToHash("7d05d08cbc596a2e5e4f13b80a743e53e09221b5323c3a61946b20873e58583f"): true,
}
