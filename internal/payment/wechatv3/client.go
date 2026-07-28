package wechatv3

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	baseURL = "https://api.mch.weixin.qq.com"
)

// Config 微信支付 V3 客户端配置。
type Config struct {
	MchID    string
	SerialNo string
	CertPath string
	KeyPath  string
	APIKey   string // APIv3 密钥，用于回调解密
}

// Client 微信支付 V3 客户端。
type Client struct {
	cfg    Config
	signer *Signer
	http   *http.Client
	certMu sync.Mutex
	certs  map[string]*certEntry // key 为 serial_no
}

type certEntry struct {
	cert      *x509.Certificate
	expiresAt time.Time
}

// NewClient 创建 V3 客户端。若未启用则返回 nil。
func NewClient(cfg Config) (*Client, error) {
	if cfg.MchID == "" || cfg.KeyPath == "" || cfg.SerialNo == "" {
		return nil, fmt.Errorf("微信支付 V3 配置不完整")
	}
	signer, err := LoadSigner(cfg.MchID, cfg.SerialNo, cfg.KeyPath)
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg:    cfg,
		signer: signer,
		http:   &http.Client{Timeout: 15 * time.Second},
		certs:  make(map[string]*certEntry),
	}, nil
}

// Do 发送带 V3 签名的 HTTP 请求，返回状态码和响应体。
func (c *Client) Do(method, path string, body []byte) (int, []byte, error) {
	u, err := url.Parse(baseURL + path)
	if err != nil {
		return 0, nil, err
	}
	bodyStr := string(body)
	if body == nil {
		bodyStr = ""
	}

	auth, err := c.signer.BuildAuthorization(method, u.RequestURI(), bodyStr)
	if err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("微信 V3 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("读取微信 V3 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return resp.StatusCode, respBody, parseAPIError(resp.StatusCode, respBody)
	}
	return resp.StatusCode, respBody, nil
}

// Signer 返回内部的签名器（供外部做 prepay 二次签名）。
func (c *Client) Signer() *Signer { return c.signer }

// APIKey 返回 APIv3 密钥（供回调解密）。
func (c *Client) APIKey() string { return c.cfg.APIKey }

// MchID 返回商户号。
func (c *Client) MchID() string { return c.cfg.MchID }

// --- 工具函数 ---

// nowUnix 返回当前 Unix 时间戳（秒）。
func nowUnix() int64 { return time.Now().Unix() }

// randomNonce 生成 hex 随机字符串。
func randomNonce(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// YuanToFen 元转分（整数）。
func YuanToFen(yuan float64) int {
	return int(math.Round(yuan * 100))
}

// FenToYuan 分转元。
func FenToYuan(fen int) float64 {
	return float64(fen) / 100.0
}
