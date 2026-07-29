package wechatv3

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 使用真实 /v3/certificates 响应验证：nonce 为明文字符串（UTF-8 字节作 IV），勿 base64 解码。
func TestDecryptResource_platformCert(t *testing.T) {
	raw, err := os.ReadFile("/tmp/wechat_certs.json")
	if err != nil {
		t.Skip("跳过：无 /tmp/wechat_certs.json，可先运行 cmd/checkwechat 生成")
	}
	apiKey := os.Getenv("WECHAT_PAY_API_KEY")
	if apiKey == "" {
		t.Skip("跳过：未设置 WECHAT_PAY_API_KEY")
	}

	var resp struct {
		Data []struct {
			EncryptCertificate Resource `json:"encrypt_certificate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("无证书数据")
	}

	plain, err := DecryptResource(apiKey, &resp.Data[0].EncryptCertificate)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !strings.HasPrefix(string(plain), "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("解密结果不是 PEM 证书")
	}
}
