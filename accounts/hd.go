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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// DefaultrootDerivationPath是自定义推导终点的根路径
//附加。因此，第一个帐户将在M/44'/60'/0'/0，第二个帐户
//在M/44'/60'/0'/1，等等。
var DefaultRootDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

// DefaultBaseadErivationPath是自定义推导端点的基本路径
//递增。因此，第一个帐户将在M/44'/60'/0'/0/0，第二个帐户
//在M/44'/60'/0'/0/1，等等，等等。
var DefaultBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0, 0}

// LegacyledGerbasederivationPath是自定义推导的旧基础路径
//端点会增加。因此，第一个帐户将在M/44'/60'/0'/0，
//第二个位于M/44'/60'/0'/1，等等，等等。
var LegacyLedgerBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

//推导Path代表层次结构的计算机友好版本
//确定的钱包帐户派生路径。
//
// BIP-32规格https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki
//将派生路径定义为形式：
//
// m / perim' / coin_type' / account' / change / address_index
//
// BIP-44规格https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki
//定义加密货币的`目的'be 44'（或0x8000002c）和
// slip-44 https://github.com/satoshilabs/slips/blob/master/master/slip-0044.md分配
//以太坊的“ COIN_TYPE” 60'（或0x8000003c）。
//
//根据规范，以太坊的根路径为m/44'/60'/0'/0
//来自https://github.com/ethereum/eips/issues/84，尽管它不是在石头上设置的
//但是帐户是应增加最后一个部分还是
// 那。我们将采用更简单的方法来增加最后一个组件。
type DerivationPath []uint32

// parsederivationPath将用户指定的派生路径字符串转换为
//内部二进制表示。
//
//全推导路径需要从`m/`前缀，相对推导开始
//路径（将附加到默认根路径）不得具有前缀
//在第一个元素的前面。空格被忽略。
func ParseDerivationPath(path string) (DerivationPath, error) {
	var result DerivationPath

	// 处理绝对或相对路径
	components := strings.Split(path, "/")
	switch {
	case len(components) == 0:
		return nil, errors.New("empty derivation path")

	case strings.TrimSpace(components[0]) == "":
		return nil, errors.New("ambiguous path: use 'm/' prefix for absolute paths, or no leading '/' for relative ones")

	case strings.TrimSpace(components[0]) == "m":
		components = components[1:]

	default:
		result = append(result, DefaultRootDerivationPath...)
	}
	// 所有剩余的组件都是相对的
	if len(components) == 0 {
		return nil, errors.New("empty derivation path") // 空的相对路径
	}
	for _, component := range components {
		// 忽略任何用户添加的空格
		component = strings.TrimSpace(component)
		var value uint32

		//处理硬性路径
		if strings.HasSuffix(component, "'") {
			value = 0x80000000
			component = strings.TrimSpace(strings.TrimSuffix(component, "'"))
		}
		//处理非硬化组件
		bigval, ok := new(big.Int).SetString(component, 0)
		if !ok {
			return nil, fmt.Errorf("invalid component: %s", component)
		}
		max := math.MaxUint32 - value
		if bigval.Sign() < 0 || bigval.Cmp(big.NewInt(int64(max))) > 0 {
			if value == 0 {
				return nil, fmt.Errorf("component %v out of allowed range [0, %d]", bigval, max)
			}
			return nil, fmt.Errorf("component %v out of allowed hardened range [0, %d]", bigval, max)
		}
		value += uint32(bigval.Uint64())

		// Append and repeat
		result = append(result, value)
	}
	return result, nil
}

//字符串实现纵梁界面，转换二进制派生路径
//到其规范表示。
func (path DerivationPath) String() string {
	result := "m"
	for _, component := range path {
		var hardened bool
		if component >= 0x80000000 {
			component -= 0x80000000
			hardened = true
		}
		result = fmt.Sprintf("%s/%d", result, component)
		if hardened {
			result += "'"
		}
	}
	return result
}

//元帅将派生路径变成其JSON-Serialized String
func (path DerivationPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(path.String())
}

// 将JSON序列化的字符串返回到派生路径
func (path *DerivationPath) UnmarshalJSON(b []byte) error {
	var dp string
	var err error
	if err = json.Unmarshal(b, &dp); err != nil {
		return err
	}
	*path, err = ParseDerivationPath(dp)
	return err
}

// Defaulterator创建一个BIP-32路径迭代器，该迭代器通过增加最后一个组件而进行：
//即M/44'/60'/0'/0/0，M/44'/60'/0'/0/1，M/44'/60'/0'/0'/0/2，..M/44'/60'/0'/0/n。
func DefaultIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))
	copy(path[:], base[:])
	// Set it back by one, so the first call gives the first result
	path[len(path)-1]--
	return func() DerivationPath {
		path[len(path)-1]++
		return path
	}
}

// LEDGERLIVEITERATOR为LEDGER LIVE创建BIP44路径迭代器。
// LEDGER实时增量第三个组件而不是第五个组件
//即M/44'/60'/0'/0/0，M/44'/60'/1'/0/0，M/44'/60'/2'/2'/0/0，..M/44'/60'/n'/0/0。
func LedgerLiveIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))
	copy(path[:], base[:])
	// Set it back by one, so the first call gives the first result
	path[2]--
	return func() DerivationPath {
		// ledgerLivePathIterator iterates on the third component
		path[2]++
		return path
	}
}
