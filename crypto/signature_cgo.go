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

// go：build！nacl &&！JS && cgo &&！gofuzz
// +build！nacl，！js，cgo，！gofuzz

package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)
// ecrecover返回创建给定签名的未压缩公钥。
func Ecrecover(hash, sig []byte) ([]byte, error) {
	return secp256k1.RecoverPubkey(hash, sig)
}

// Sigtopub返回创建给定签名的公共密钥。
func SigToPub(hash, sig []byte) (*ecdsa.PublicKey, error) {
	s, err := Ecrecover(hash, sig)
	if err != nil {
		return nil, err
	}

	x, y := elliptic.Unmarshal(S256(), s)
	return &ecdsa.PublicKey{Curve: S256(), X: x, Y: y}, nil
}

//符号计算ECDSA签名。
//
//此功能容易受到可能泄漏的明文攻击
//有关用于签名的私钥的信息。呼叫者必须
//请注意，给定的摘要不能由对手选择。常见的
//解决方案是在计算签名之前放置任何输入。
//
//产生的签名在[r ||S ||v]格式，其中v为0或1。
func Sign(digestHash []byte, prv *ecdsa.PrivateKey) (sig []byte, err error) {
	if len(digestHash) != DigestLength {
		return nil, fmt.Errorf("hash is required to be exactly %d bytes (%d)", DigestLength, len(digestHash))
	}
	seckey := math.PaddedBigBytes(prv.D, prv.Params().BitSize/8)
	defer zeroBytes(seckey)
	return secp256k1.Sign(digestHash, seckey)
}

//验证符号检查给定的公钥是否通过摘要创建了签名。
//公共密钥应采用压缩（33个字节）或未压缩（65个字节）格式。
//签名应具有64个字节[r ||S]格式。
func VerifySignature(pubkey, digestHash, signature []byte) bool {
	return secp256k1.VerifySignature(pubkey, digestHash, signature)
}

// DecompressPubkey parses a public key in the 33-byte compressed format.
func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	x, y := secp256k1.DecompressPubkey(pubkey)
	if x == nil {
		return nil, fmt.Errorf("invalid public key")
	}
	return &ecdsa.PublicKey{X: x, Y: y, Curve: S256()}, nil
}

// CompressPubkey编码33字节压缩格式的公共密钥。
func CompressPubkey(pubkey *ecdsa.PublicKey) []byte {
	return secp256k1.CompressPubkey(pubkey.X, pubkey.Y)
}

// S256返回SECP256K1曲线的实例。
func S256() elliptic.Curve {
	return secp256k1.S256()
}
