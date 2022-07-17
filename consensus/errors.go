//版权所有2017年作者
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

package consensus

import "errors"

var (
	// 验证块需要祖先时返回errunknownancestor
//那是未知的。
	ErrUnknownAncestor = errors.New("unknown ancestor")

	// 验证块需要祖先时返回Errprunedancestor
	//这是已知的，但其状态不可用。
	ErrPrunedAncestor = errors.New("pruned ancestor")

	// 当块的时间戳将来返回ErrFutureBlock
	//到当前节点。
	ErrFutureBlock = errors.New("block in the future")

	// 如果一个块的号码不等于其父的，则返回errinvalidnumber
	// 加一。
	ErrInvalidNumber = errors.New("invalid block number")

	// 如果块无效WRT，则返回Errinvalidterminalblock。终点站
//总难度。
	ErrInvalidTerminalBlock = errors.New("invalid terminal block")
)
