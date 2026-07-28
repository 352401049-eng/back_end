package wechatv3

import (
	"encoding/json"
	"fmt"
)

// PaySuccessNotify 支付成功回调解密后的内容（resource 解密后）。
type PaySuccessNotify struct {
	AppID         string `json:"appid"`
	MchID         string `json:"mchid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"`
	TradeType     string `json:"trade_type"`
	SuccessTime   string `json:"success_time"` // RFC3339
	Attach        string `json:"attach"`
	Amount        struct {
		Total         int    `json:"total"`
		PayerTotal    int    `json:"payer_total"`
		Currency      string `json:"currency"`
		PayerCurrency string `json:"payer_currency"`
	} `json:"amount"`
	Payer struct {
		OpenID string `json:"openid"`
	} `json:"payer"`
}

// RefundSuccessNotify 退款成功回调解密后的内容。
type RefundSuccessNotify struct {
	MchID       string `json:"mchid"`
	OutTradeNo  string `json:"out_trade_no"`
	OutRefundNo string `json:"out_refund_no"`
	RefundID    string `json:"refund_id"`
	RefundStatus string `json:"refund_status"` // SUCCESS
	SuccessTime string `json:"success_time"`
	Amount      struct {
		Total       int `json:"total"`
		Refund      int `json:"refund"`
		PayerTotal  int `json:"payer_total"`
		PayerRefund int `json:"payer_refund"`
	} `json:"amount"`
}

// Event 事件类型常量。
const (
	EventPaySuccess    = "TRANSACTION.SUCCESS"
	EventRefundSuccess = "REFUND.SUCCESS"
)

// ParseAndDecryptNotify 解析回调 JSON、验签、解密 resource。
// 返回 event_type 和解密后的 JSON 字节。
func (c *Client) ParseAndDecryptNotify(headers map[string]string, body []byte) (string, []byte, error) {
	timestamp := headers["Wechatpay-Timestamp"]
	nonce := headers["Wechatpay-Nonce"]
	signature := headers["Wechatpay-Signature"]
	serial := headers["Wechatpay-Serial"]

	if timestamp == "" || nonce == "" || signature == "" || serial == "" {
		return "", nil, fmt.Errorf("回调请求头不完整")
	}

	// 1. 验签
	cert, err := c.GetPlatformCert(serial)
	if err != nil {
		return "", nil, fmt.Errorf("获取平台证书失败: %w", err)
	}
	signing := BuildNotifySign(timestamp, nonce, string(body))
	if err := VerifySignature(cert, signing, signature); err != nil {
		return "", nil, fmt.Errorf("回调验签失败: %w", err)
	}

	// 2. 解析回调体
	cb, err := parseCallbackBody(body)
	if err != nil {
		return "", nil, err
	}

	// 3. 解密 resource
	plaintext, err := DecryptResource(c.cfg.APIKey, &cb.Resource)
	if err != nil {
		return "", nil, fmt.Errorf("解密回调 resource 失败: %w", err)
	}

	return cb.EventType, plaintext, nil
}

// UnmarshalPaySuccess 将解密后的 JSON 解析为支付成功通知。
func UnmarshalPaySuccess(data []byte) (*PaySuccessNotify, error) {
	var n PaySuccessNotify
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("解析支付通知失败: %w", err)
	}
	return &n, nil
}

// UnmarshalRefundSuccess 将解密后的 JSON 解析为退款成功通知。
func UnmarshalRefundSuccess(data []byte) (*RefundSuccessNotify, error) {
	var n RefundSuccessNotify
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("解析退款通知失败: %w", err)
	}
	return &n, nil
}
