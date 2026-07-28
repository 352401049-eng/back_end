package wechatv3

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sync"
	"time"
)

const certPath = "/v3/certificates"

type certsResponse struct {
	Data []certItem `json:"data"`
}

type certItem struct {
	SerialNo          string             `json:"serial_no"`
	EffectiveTime     string             `json:"effective_time"`
	ExpireTime        string             `json:"expire_time"`
	EncryptCertificate encryptCertificate `json:"encrypt_certificate"`
}

// FetchPlatformCerts 下载并缓存微信平台证书。应在启动时调用一次，之后定期刷新。
// 每次刷新时全量替换缓存，避免残留已吊销的旧证书。
func (c *Client) FetchPlatformCerts() error {
	_, body, err := c.Do("GET", certPath, nil)
	if err != nil {
		return fmt.Errorf("获取平台证书失败: %w", err)
	}
	var resp certsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("解析平台证书响应失败: %w", err)
	}

	newCerts := make(map[string]*certEntry)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for _, item := range resp.Data {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			certPEM, err := decryptCert(c.cfg.APIKey, &item.EncryptCertificate)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("解密证书 %s 失败: %w", item.SerialNo, err)
				}
				mu.Unlock()
				return
			}
			block, _ := pem.Decode(certPEM)
			if block == nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("证书 %s PEM 解析失败", item.SerialNo)
				}
				mu.Unlock()
				return
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("证书 %s 解析失败: %w", item.SerialNo, err)
				}
				mu.Unlock()
				return
			}
			expireAt, _ := time.Parse(time.RFC3339, item.ExpireTime)
			if expireAt.IsZero() {
				expireAt = time.Now().AddDate(1, 0, 0)
			}
			mu.Lock()
			newCerts[item.SerialNo] = &certEntry{cert: cert, expiresAt: expireAt}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	// 全量替换：已吊销/过期的旧证书不会残留
	c.certMu.Lock()
	c.certs = newCerts
	c.certMu.Unlock()
	return nil
}

// GetPlatformCert 按序列号获取缓存的平台证书。
func (c *Client) GetPlatformCert(serialNo string) (*x509.Certificate, error) {
	c.certMu.Lock()
	entry, ok := c.certs[serialNo]
	c.certMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("未找到序列号 %s 的平台证书", serialNo)
	}
	if time.Now().After(entry.expiresAt) {
		return nil, fmt.Errorf("序列号 %s 的平台证书已过期", serialNo)
	}
	return entry.cert, nil
}
