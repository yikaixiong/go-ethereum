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
	"strings"
)

// URL表示钱包或帐户的规范识别URL。
//
//这是url.url的简化版本，具有重要的限制（
//在这里被视为功能），它仅包含可取值的组件，
//以及它不执行特殊字符的任何URL编码/解码。
//
//前者很重要，允许复制帐户而不离开现场
//引用原始版本，而后者对于确保
//一种单一的规范形式，与RFC 3986规格相反，与许多允许的形式相反。
//
//因此，这些URL不应在以太坊范围之外使用
//钱包或帐户。
type URL struct {
	Scheme string // 协议方案以识别功能强大的帐户后端
	Path   string //后端识别独特实体的路径
}

// Parseurl将提供的用户URL转换为特定帐户结构。
func parseURL(url string) (URL, error) {
	parts := strings.Split(url, "://")
	if len(parts) != 2 || parts[0] == "" {
		return URL{}, errors.New("protocol scheme missing")
	}
	return URL{
		Scheme: parts[0],
		Path:   parts[1],
	}, nil
}

// 字符串实现纵梁界面。
func (u URL) String() string {
	if u.Scheme != "" {
		return fmt.Sprintf("%s://%s", u.Scheme, u.Path)
	}
	return u.Path
}

// 终端引以实现log.terminalstringer接口。
func (u URL) TerminalString() string {
	url := u.String()
	if len(url) > 32 {
		return url[:31] + ".."
	}
	return url
}

// Marshaljson实现JSON.MARSHALLER界面。
func (u URL) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// Unmarshaljson解析URL。
func (u *URL) UnmarshalJSON(input []byte) error {
	var textURL string
	err := json.Unmarshal(input, &textURL)
	if err != nil {
		return err
	}
	url, err := parseURL(textURL)
	if err != nil {
		return err
	}
	u.Scheme = url.Scheme
	u.Path = url.Path
	return nil
}

// CMP比较X和Y和返回：
//
//   -1 if x <  y
//    0 if x == y
//   +1 if x >  y
//
func (u URL) Cmp(url URL) int {
	if u.Scheme == url.Scheme {
		return strings.Compare(u.Path, url.Path)
	}
	return strings.Compare(u.Scheme, url.Scheme)
}
