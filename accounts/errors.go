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

// ErrUnknownWallet is returned for any requested operation for which no backend
// provides the specified wallet.
var ErrUnknownWallet = errors.New("unknown wallet")

// ErrNotSupported is returned when an operation is requested from an account
// backend that it does not support.
var ErrNotSupported = errors.New("not supported")

// ErrInvalidPassphrase is returned when a decryption operation receives a bad
// passphrase.
var ErrInvalidPassphrase = errors.New("invalid password")

// ErrWalletAlreadyOpen is returned if a wallet is attempted to be opened the
// second time.
var ErrWalletAlreadyOpen = errors.New("wallet already open")

// ErrWalletClosed is returned if a wallet is offline.
var ErrWalletClosed = errors.New("wallet closed")

// AuthNeededError is returned by backends for signing requests where the user
// is required to provide further authentication before signing can succeed.
//
// This usually means either that a password needs to be supplied, or perhaps a
// one time PIN code displayed by some hardware device.
type AuthNeededError struct {
	Needed string // Extra authentication the user needs to provide
}

// NewAuthNeededError creates a new authentication error with the extra details
// about the needed fields set.
func NewAuthNeededError(needed string) error {
	return &AuthNeededError{
		Needed: needed,
	}
}

// Error implements the standard error interface.
func (err *AuthNeededError) Error() string {
	return fmt.Sprintf("authentication needed: %s", err.Needed)
}
