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

//包装帐户实施高级以太坊帐户管理。
package accounts

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"golang.org/x/crypto/sha3"
)

//帐户代表位于特定位置定义的以太坊帐户
//由可选的URL字段。
type Account struct {
	Address common.Address `json:"address"` // 以太坊帐户地址从密钥中得出
	URL     URL            `json:"url"`     // 后端内的可选资源定位器
}

const (
	MimetypeDataWithValidator = "data/validator"
	MimetypeTypedData         = "data/typed"
	MimetypeClique            = "application/x-clique-header"
	MimetypeTextPlain         = "text/plain"
)

//钱包代表可能包含一个或多个的软件或硬件钱包
//帐户（源自同一种子）。
type Wallet interface {
	// URL检索该钱包可到达的规范路径。这是
	//上层用来定义从多个钱包上的所有钱包上的分类顺序
	//后端。
	URL() URL

	//状态返回文本状态以帮助用户以当前状态
	// 钱包。它还返回一个错误，表明钱包可能有任何故障
	// 遭遇。
	Status() (string, error)

	//打开初始化对钱包实例的访问。它并不是要解锁或
	//解密帐户键，而只是为了建立与硬件的连接
	//钱包和/或访问衍生种子。
	//
	//密码参数可能会或可能不会通过实现
	//特定的钱包实例。没有无密码打开方法的原因
	//要努力朝着统一的钱包处理努力，忽略了不同的钱
	//后端提供商。
	//
	//请注意，如果您打开钱包，则必须将其关闭以释放任何分配的钱
	//资源（使用硬件钱包时尤为重要）。
	Open(passphrase string) error

	//关闭释放开放钱包实例持有的任何资源。
	Close() error

	//Accounts 检索钱包当前知道的签名账户列表
	//的。对于分层确定性钱包，该列表并不详尽，
	//而是仅包含在帐户派生期间显式固定的帐户。
	Accounts() []Account

	//包含返回一个帐户是否是这个特定钱包的一部分。
	Contains(account Account) bool

	//推导尝试明确得出层次确定性帐户的尝试
//指定的推导路径。如果要求，将添加派生帐户
//到钱包的跟踪帐户列表。
	Derive(path DerivationPath, pin bool) (Account, error)

	//自我设置的基本帐户派生路径
//发现非零帐户并自动将其添加到跟踪的列表中
//帐户。
//
//注意，自推导将增加指定路径的最后一个组件
//反对下降进入儿童路径以发现启动帐户
//来自非零组件。
//
//一些硬件钱包通过其演变切换了推导路径，因此
//此方法支持提供多个基础来发现旧用户帐户
// 也。只有最后一个基础将用于得出下一个空帐户。
//
//您可以通过用零来调用自我来发现自动帐户发现
//连锁状态读者。
	SelfDerive(bases []DerivationPath, chain ethereum.ChainStateReader)

	// Signdata请钱包签署给定数据的哈希
//它仅通过其内部包含的地址指定的帐户查找帐户
//或借助嵌入式URL字段的任何位置元数据。
//
//如果钱包需要其他身份验证才能签署请求（例如
//密码解密帐户或PIN代码以验证交易），
//将返回AuthneedEderror实例，其中包含用户的Infos
//关于需要哪些字段或操作。用户可以通过提供
//通过SignDatawithPassphrase或其他方式（例如，解锁）所需的细节
//在密钥库中的帐户）。
	SignData(account Account, mimeType string, data []byte) ([]byte, error)

	// signdatawithpassphrase与signdata相同，但也采用了一个密码
	//注意：错误的呼叫可能会误认为这两个字符串，并且
	//在Mimetype字段中提供密码，反之亦然。因此，实施
	//绝不应该回声模拟型或返回错误响应中的模拟型
	SignDataWithPassphrase(account Account, passphrase, mimeType string, data []byte) ([]byte, error)

	// SIGNTEXT请求钱包以签署给定数据的哈希（前缀）
	//通过以太坊前缀方案
	//它仅通过其内部包含的地址指定的帐户查找帐户
	//或借助嵌入式URL字段的任何位置元数据。
	//
	//如果钱包需要其他身份验证才能签署请求（例如
	//密码解密帐户或PIN代码以验证交易），
	//将返回AuthneedEderror实例，其中包含用户的Infos
	//关于需要哪些字段或操作。用户可以通过提供
	//通过SigntextWithPassphrase或其他方式（例如，解锁）所需的细节
	//在密钥库中的帐户）。
	//
	//此方法应使用V 0或1返回“规范”格式的签名。
	SignText(account Account, text []byte) ([]byte, error)

	// SignTextWithPassphrase与SignText相同，但也采用了一个密码	
	SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error)

	// SIGNTX请求钱包签署给定的交易。
	//
	//它仅通过其内部包含的地址指定的帐户查找帐户
	//或借助嵌入式URL字段的任何位置元数据。
	//
	//如果钱包需要其他身份验证才能签署请求（例如
	//密码解密帐户或PIN代码以验证交易），
	//将返回AuthneedEderror实例，其中包含用户的Infos
	//关于需要哪些字段或操作。用户可以通过提供
	//所需的详细信息通过SigntxWithPassphrase或其他方式（例如，解锁
	//在密钥库中的帐户）。
	SignTx(account Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)

	// SIGNTXWITHPASSPHRASE与SIGNTX相同，但也采用了一个密码
	SignTxWithPassphrase(account Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)
}

//后端是一个“钱包提供商”，可能包含一批他们可以的帐户
//与并应要求签署交易，这样做。
type Backend interface {
	//Wallets 检索后端当前知道的钱包列表。
	//
	//返回的钱包默认不打开。对于软件高清钱包，这
	//意味着没有基础种子被解密，对于硬件钱包来说，没有实际的
	//连接建立。
	//
	//生成的钱包列表将根据其内部按字母顺序排序
	//由后端分配的 URL。由于钱包（尤其是硬件）可能会出现并且
	//go, 同一个钱包可能会出现在列表中的不同位置
	//后续检索。
	Wallets() []Wallet

	//订阅创建一个异步订阅以接收通知，当
	//后端检测钱包的到达或离开。
	Subscribe(sink chan<- WalletEvent) event.Subscription
}

// TexThash是一个辅助功能，可以计算给定消息的哈希
//安全地用于计算签名。
//
//哈希计算为
// keccak256（“ \ x19ethereum签名消息：\ n” $ {message Length} $ {message}）。
//
//这给出了签名消息的上下文，并防止交易的签名。
func TextHash(data []byte) []byte {
	hash, _ := TextAndHash(data)
	return hash
}

// textandhash是一个辅助功能，可以计算给定消息的哈希
//安全地用于计算签名。
//
//哈希计算为
// keccak256（“ \ x19ethereum签名消息：\ n” $ {message Length} $ {message}）。
//
//这给出了签名消息的上下文，并防止交易的签名。
func TextAndHash(data []byte) ([]byte, string) {
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(data), string(data))
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(msg))
	return hasher.Sum(nil), msg
}
// WalletEventType表示可以发射的不同事件类型
//钱包订阅子系统。
type WalletEventType int

const (
	//当通过USB或通过USB检测到新钱包时，将发射软件
//密钥库中的文件系统事件。
	WalletArrived WalletEventType = iota

//按照目的成功打开钱包时，将钱包发射
//启动任何背景过程，例如自动键推导。
	WalletOpened

	// 钱包
	WalletDropped
)

// Whatletevent是钱包到达或
//检测到出发。
type WalletEvent struct {
	Wallet Wallet          // 钱包实例到达或离开
	Kind   WalletEventType // 系统中发生的事件类型
}
