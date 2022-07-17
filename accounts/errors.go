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

package accounts

import (
	"errors"
	"fmt"
)
//返回的任何要求的操作都没有后端
//提供指定的帐户。
var ErrUnknownAccount = errors.New("unknown account")

// 返回任何请求的操作，返回errunknownwallet
//提供指定的钱包。
var ErrUnknownWallet = errors.New("unknown wallet")

// 从帐户请求操作时返回errnotsupported
//后端不支持它。
var ErrNotSupported = errors.New("not supported")

// 解密操作收到不良
//密码。
var ErrInvalidPassphrase = errors.New("invalid password")

// 如果试图打开钱包，将返回errwalletalReveOpen
// 第二次。
var ErrWalletAlreadyOpen = errors.New("wallet already open")

// 如果钱包脱机，将返回errwalletcled。
var ErrWalletClosed = errors.New("wallet closed")

// AuthneededError由后端返回，用于签署用户的签名请求
//必须在签名成功之前提供进一步的身份验证。
//
//这通常意味着需要提供密码，或者可能是
//某些硬件设备显示的一次性PIN代码。
type AuthNeededError struct {
	Needed string // 用户需要提供的额外认证
}

// NewauthneededeRror创建了一个新的身份验证错误，并提供了额外的详细信息
//关于所需的字段设置。
func NewAuthNeededError(needed string) error {
	return &AuthNeededError{
		Needed: needed,
	}
}

// Error implements the standard error interface.
func (err *AuthNeededError) Error() string {
	return fmt.Sprintf("authentication needed: %s", err.Needed)
}
