//版权2020年作者
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

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/ethereum/go-ethereum/signer/fourbyte"
)

// TODO:  初始化
func init() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:", os.Args[0], "<hexdata>")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Parses the given ABI data and tries to interpret it from the fourbyte database.`)
	}
}

// TODO : 
func parse(data []byte) {
	db, err := fourbyte.New()
	if err != nil {
		die(err)
	}
	messages := apitypes.ValidationMessages{}
	db.ValidateCallData(nil, data, &messages)
	for _, m := range messages.Messages {
		fmt.Printf("%v: %v\n", m.Typ, m.Message)
	}
}

// Example
// ./abidump a9059cbb000000000000000000000000ea0e2dc7d65a50e77fc7e84bff3fd2a9e781ff5c0000000000000000000000000000000000000000000000015af1d78b58c40000

// TODO:  主函数
func main() {
	flag.Parse()

	switch {
	case flag.NArg() == 1:
		hexdata := flag.Arg(0)
		data, err := hex.DecodeString(strings.TrimPrefix(hexdata, "0x"))
		if err != nil {
			die(err)
		}
		parse(data)
	default:
		fmt.Fprintln(os.Stderr, "Error: one argument needed")
		flag.Usage()
		os.Exit(2)
	}
}

// TODO: die 死亡
func die(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
