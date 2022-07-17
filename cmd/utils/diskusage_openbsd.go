//版权2021 The Go-Ethereum作者
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

// GO：构建OpenBSD
// +构建OpenBSD

package utils

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// TODO: 获取免费的磁盘空间
func getFreeDiskSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("failed to call Statfs: %v", err)
	}

	// 可用块 *大小每个块=字节中的可用空间
	var bavail = stat.F_bavail
	// 不确定OpenBSD是否需要以下检查
	if stat.F_bavail < 0 {
		// FreeBSD可以有负数的可用块
		//由于宽限期的限制。
		bavail = 0
	}
	// Uncortervent Unconvert
	return uint64(bavail) * uint64(stat.F_bsize), nil
}
