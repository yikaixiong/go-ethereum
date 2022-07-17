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

package fdlimit

import "fmt"

// hardlimit is the number of file descriptors allowed at max by the kernel.
const hardlimit = 16384

// Raise tries to maximize the file descriptor allowance of this process
// to the maximum hard-limit allowed by the OS.
func Raise(max uint64) (uint64, error) {
	// This method is NOP by design:
	//  * Linux/Darwin counterparts need to manually increase per process limits
	//  * On Windows Go uses the CreateFile API, which is limited to 16K files, non
	//    changeable from within a running process
	// This way we can always "request" raising the limits, which will either have
	// or not have effect based on the platform we're running on.
	if max > hardlimit {
		return hardlimit, fmt.Errorf("file descriptor limit (%d) reached", hardlimit)
	}
	return max, nil
}

// 当前检索此文件允许打开的文件描述符数
// 过程。
func Current() (int, error) {
	// Please see Raise for the reason why we use hard coded 16K as the limit
	return hardlimit, nil
}

//最大检索文件描述符的最大数量此过程是
//允许自身请求。
func Maximum() (int, error) {
	return Current()
}
