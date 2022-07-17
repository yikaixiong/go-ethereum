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

//软件包utils包含用于Go-Ethereum命令的内部辅助功能。
package utils

import (
	"fmt"

	"github.com/ethereum/go-ethereum/console/prompt"
)

// getPassphrase向用户显示给定文本（提示）并请求一些文本
//要输入的数据，但不能将其回音到终端中。
//该方法返回用户提供的输入。
func GetPassPhrase(text string, confirmation bool) string {
	if text != "" {
		fmt.Println(text)
	}
	password, err := prompt.Stdin.PromptPassword("Password: ")
	if err != nil {
		Fatalf("Failed to read password: %v", err)
	}
	if confirmation {
		confirm, err := prompt.Stdin.PromptPassword("Repeat password: ")
		if err != nil {
			Fatalf("Failed to read password confirmation: %v", err)
		}
		if password != confirm {
			Fatalf("Passwords do not match")
		}
	}
	return password
}

// getPassphraseWithList检索与帐户关联的密码，要么获取
//从预加载的密码套件列表中，或从用户交互要求。
func GetPassPhraseWithList(text string, confirmation bool, index int, passwords []string) string {
	// If a list of passwords was supplied, retrieve from them
	if len(passwords) > 0 {
		if index < len(passwords) {
			return passwords[index]
		}
		return passwords[len(passwords)-1]
	}
	// Otherwise prompt the user for the password
	password := GetPassPhrase(text, confirmation)
	return password
}
