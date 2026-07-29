package wechatv3

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// NormalizeAPIKey 规范化 APIv3 密钥：去空白，校验必须为 32 字节。
func NormalizeAPIKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("APIv3 密钥未配置")
	}
	if len(key) != 32 {
		return "", fmt.Errorf("APIv3 密钥长度必须为 32 字符（当前 %d），请检查 WECHAT_PAY_API_KEY 是否与商户平台「API安全 → APIv3密钥」完全一致", len(key))
	}
	return key, nil
}

// ReadCertSerialNo 从 apiclient_cert.pem 读取证书序列号（大写十六进制）。
func ReadCertSerialNo(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("读取商户证书失败: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("商户证书 PEM 解析失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("解析商户证书失败: %w", err)
	}
	return strings.ToUpper(fmt.Sprintf("%X", cert.SerialNumber)), nil
}

// VerifyAPIKey 调用 GET /v3/certificates 并尝试解密，验证 APIv3 密钥是否正确。
func (c *Client) VerifyAPIKey() error {
	if c.cfg.APIKey == "" {
		return fmt.Errorf("APIv3 密钥未配置")
	}
	_, body, err := c.Do("GET", certPath, nil)
	if err != nil {
		return fmt.Errorf("请求平台证书失败（商户私钥/证书序列号可能有问题）: %w", err)
	}
	var resp certsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("解析平台证书响应失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return fmt.Errorf("微信平台未返回可用证书")
	}
	for _, item := range resp.Data {
		if _, err := decryptCert(c.cfg.APIKey, &item.EncryptCertificate); err != nil {
			return fmt.Errorf("APIv3 密钥验证失败: %w（请到 pay.weixin.qq.com → 账户中心 → API安全 → 设置APIv3密钥，确保与 WECHAT_PAY_API_KEY 完全一致；注意不是 APIv2 密钥）", err)
		}
	}
	return nil
}
