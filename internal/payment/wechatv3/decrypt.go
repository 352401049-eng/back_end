package wechatv3

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Resource 微信回调的加密 resource 字段。
type Resource struct {
	Algorithm      string `json:"algorithm"`
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
	Nonce          string `json:"nonce"`
	OriginalType   string `json:"original_type"`
}

// DecryptResource 用 APIv3 密钥解密回调 resource。
// AEAD_AES_256_GCM：nonce 12 字节，associated_data 为附加数据。
func DecryptResource(apiKey string, r *Resource) ([]byte, error) {
	if r.Algorithm != "AEAD_AES_256_GCM" {
		return nil, fmt.Errorf("不支持的加密算法: %s", r.Algorithm)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(r.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("ciphertext base64 解码失败: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(r.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce base64 解码失败: %w", err)
	}

	block, err := aes.NewCipher([]byte(apiKey))
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 失败: %w", err)
	}

	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, []byte(r.AssociatedData))
	if err != nil {
		return nil, fmt.Errorf("解密失败: %w", err)
	}
	return plaintext, nil
}

// DecryptCert 解密平台证书（GET /v3/certificates 返回的 encrypt_certificate）。
type encryptCertificate struct {
	Algorithm      string `json:"algorithm"`
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
	Nonce          string `json:"nonce"`
}

func decryptCert(apiKey string, ec *encryptCertificate) ([]byte, error) {
	r := &Resource{
		Algorithm:      ec.Algorithm,
		Ciphertext:     ec.Ciphertext,
		AssociatedData: ec.AssociatedData,
		Nonce:          ec.Nonce,
	}
	return DecryptResource(apiKey, r)
}

// CallbackBody 微信回调的顶层结构。
type CallbackBody struct {
	ID           string   `json:"id"`
	CreateTime   string   `json:"create_time"`
	ResourceType string   `json:"resource_type"`
	EventType    string   `json:"event_type"`
	Summary      string   `json:"summary"`
	Resource     Resource `json:"resource"`
}

// parseCallbackBody 解析回调 JSON 体。
func parseCallbackBody(raw []byte) (*CallbackBody, error) {
	var cb CallbackBody
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("解析回调 JSON 失败: %w", err)
	}
	return &cb, nil
}
