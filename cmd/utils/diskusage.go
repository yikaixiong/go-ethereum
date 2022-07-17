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

// go：build！windows &&！OpenBSD
// +build！windows，！openBSD

package utils

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// TODO: 获取空闲的磁盘
func getFreeDiskSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("failed to call Statfs: %v", err)
	}

	// Available blocks * size per block = available space in bytes
	var bavail = stat.Bavail
	// nolint:staticcheck
	if stat.Bavail < 0 {
		// FreeBSD can have a negative number of blocks available
		// because of the grace limit.
		bavail = 0
	}
	//nolint:unconvert
	return uint64(bavail) * uint64(stat.Bsize), nil
}
