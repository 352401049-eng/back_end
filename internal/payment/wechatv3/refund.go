package wechatv3

import (
	"encoding/json"
	"fmt"
)

const refundPath = "/v3/refund/domestic/refunds"

// CreateRefundRequest 退款申请请求。
type CreateRefundRequest struct {
	OutTradeNo  string         `json:"out_trade_no,omitempty"`
	TransactionID string       `json:"transaction_id,omitempty"`
	OutRefundNo string         `json:"out_refund_no"`
	Reason      string         `json:"reason,omitempty"`
	NotifyURL   string         `json:"notify_url,omitempty"`
	Amount      RefundAmount   `json:"amount"`
}

// RefundAmount 退款金额。
type RefundAmount struct {
	Refund   int    `json:"refund"`   // 退款金额（分）
	Total    int    `json:"total"`    // 原订单金额（分）
	Currency string `json:"currency"` // CNY
}

// CreateRefundResponse 退款申请响应。
type CreateRefundResponse struct {
	RefundID    string `json:"refund_id"`
	OutRefundNo string `json:"out_refund_no"`
	Status      string `json:"status"` // SUCCESS/PROCESSING/...
	Amount      struct {
		Total       int `json:"total"`
		Refund      int `json:"refund"`
		PayerTotal  int `json:"payer_total"`
		PayerRefund int `json:"payer_refund"`
	} `json:"amount"`
}

// CreateRefund 发起退款申请。
func (c *Client) CreateRefund(req *CreateRefundRequest) (*CreateRefundResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化退款请求失败: %w", err)
	}
	_, respBody, err := c.Do("POST", refundPath, body)
	if err != nil {
		return nil, fmt.Errorf("退款申请失败: %w", err)
	}
	var result CreateRefundResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析退款响应失败: %w", err)
	}
	return &result, nil
}
