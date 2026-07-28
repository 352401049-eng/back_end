package wechatv3

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
)

// Signer 持有商户私钥，负责生成 V3 签名。
type Signer struct {
	MchID    string
	SerialNo string
	key      *rsa.PrivateKey
}

// LoadSigner 从 PEM 文件加载商户私钥。
func LoadSigner(mchID, serialNo, keyPath string) (*Signer, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("读取商户私钥文件失败: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("商户私钥 PEM 解析失败")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析商户私钥失败: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("商户私钥不是 RSA 密钥")
	}
	return &Signer{MchID: mchID, SerialNo: serialNo, key: rsaKey}, nil
}

// Sign 对签名串生成 RSA-SHA256 签名，输出 base64。
func (s *Signer) Sign(signingString string) (string, error) {
	h := sha256.Sum256([]byte(signingString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("签名失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// BuildAuthorization 构造 V3 的 Authorization 请求头。
// signingString 格式: "HTTP_METHOD\nURL_PATH\nTIMESTAMP\nNONCE_STR\nBODY\n"
func (s *Signer) BuildAuthorization(method, urlPath, body string) (string, error) {
	ts := fmt.Sprintf("%d", nowUnix())
	nonce := randomNonce(32)
	signing := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n", method, urlPath, ts, nonce, body)
	sig, err := s.Sign(signing)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`,
		s.MchID, nonce, ts, s.SerialNo, sig,
	), nil
}

// SignPrepay 对 prepay_id 做二次签名，返回 wx.requestPayment 所需参数。
// 签名串：APPID\nTIMESTAMP\nNONCE\nprepay_id=PREPAY_ID\n
func (s *Signer) SignPrepay(appID, prepayID string) (map[string]interface{}, error) {
	ts := fmt.Sprintf("%d", nowUnix())
	nonce := randomNonce(32)
	pkg := "prepay_id=" + prepayID
	signing := fmt.Sprintf("%s\n%s\n%s\n%s\n", appID, ts, nonce, pkg)
	sig, err := s.Sign(signing)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"timeStamp": ts,
		"nonceStr":  nonce,
		"package":   pkg,
		"signType":  "RSA",
		"paySign":   sig,
	}, nil
}

// BuildNotifySign 构造回调验签的待签名字符串。
// 格式: "TIMESTAMP\nNONCE\nBODY\n"
func BuildNotifySign(timestamp, nonce, body string) string {
	return fmt.Sprintf("%s\n%s\n%s\n", timestamp, nonce, body)
}

// VerifySignature 用平台证书公钥验证签名。
func VerifySignature(cert *x509.Certificate, signing, signature string) error {
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("签名 base64 解码失败: %w", err)
	}
	h := sha256.Sum256([]byte(signing))
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("平台证书公钥不是 RSA")
	}
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sigBytes)
}
