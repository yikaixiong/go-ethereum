//版权所有2017年作者
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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/urfave/cli/v2"
)

// getPassphrase获得用户给出的密码。它首先检查
//-- passfile命令行标志，并最终提示用户
//密码。
func getPassphrase(ctx *cli.Context, confirmation bool) string {
	// Look for the --passwordfile flag.
	passphraseFile := ctx.String(passphraseFlag.Name)
	if passphraseFile != "" {
		content, err := os.ReadFile(passphraseFile)
		if err != nil {
			utils.Fatalf("Failed to read password file '%s': %v",
				passphraseFile, err)
		}
		return strings.TrimRight(string(content), "\r\n")
	}

	// Otherwise prompt the user for the passphrase.
	return utils.GetPassPhrase("", confirmation)
}

// MUSTPRINTJSON打印给定对象的JSON编码和
//当编制失败时，请通过错误消息退出程序。
func mustPrintJSON(jsonObject interface{}) {
	str, err := json.MarshalIndent(jsonObject, "", "  ")
	if err != nil {
		utils.Fatalf("Failed to marshal JSON object: %v", err)
	}
	fmt.Println(string(str))
}
