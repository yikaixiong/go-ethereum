//版权2022 The Go-Ethereum作者
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

package abi

import "fmt"

// ResolvenameConflict返回给定物品的下一个可用名称。
//该助手可用于许多目的：
//
//-支持坚固的功能超载，此功能可以修复
//超载功能的名称冲突。
//-在Golang绑定生成中，参数（在功能，事件，错误中，
//和struct定义）名称将转换为骆驼样式
//最终可能导致姓名冲突。
//
//名称冲突主要是通过添加数字后缀来解决的。
//例如如果ABI包含发送方法，请发送1
// resolvenameconflict将返回send2以进行输入发送。
func ResolveNameConflict(rawName string, used func(string) bool) string {
	name := rawName
	ok := used(name)
	for idx := 0; ok; idx++ {
		name = fmt.Sprintf("%s%d", rawName, idx)
		ok = used(name)
	}
	return name
}
