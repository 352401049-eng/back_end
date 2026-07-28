package wechatv3

import (
	"encoding/json"
	"fmt"
)

const prepayPath = "/v3/pay/transactions/jsapi"

// CreateJSAPIPrepayRequest JSAPI 统一下单请求。
type CreateJSAPIPrepayRequest struct {
	AppID       string                 `json:"appid"`
	MchID       string                 `json:"mchid"`
	Description string                 `json:"description"`
	OutTradeNo  string                 `json:"out_trade_no"`
	NotifyURL   string                 `json:"notify_url"`
	Amount      PrepayAmount           `json:"amount"`
	Payer       PrepayPayer            `json:"payer"`
	Attach      string                 `json:"attach,omitempty"`
	TimeExpire  string                 `json:"time_expire,omitempty"` // RFC3339
}

// PrepayAmount 支付金额。
type PrepayAmount struct {
	Total    int    `json:"total"`    // 单位：分
	Currency string `json:"currency"` // CNY
}

// PrepayPayer 支付者。
type PrepayPayer struct {
	OpenID string `json:"openid"`
}

// CreateJSAPIPrepayResponse JSAPI 统一下单响应。
type CreateJSAPIPrepayResponse struct {
	PrepayID string `json:"prepay_id"`
}

// CreateJSAPIPrepay 发起 JSAPI 统一下单，返回 prepay_id。
func (c *Client) CreateJSAPIPrepay(req *CreateJSAPIPrepayRequest) (*CreateJSAPIPrepayResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化预支付请求失败: %w", err)
	}
	_, respBody, err := c.Do("POST", prepayPath, body)
	if err != nil {
		return nil, fmt.Errorf("JSAPI 下单失败: %w", err)
	}
	var result CreateJSAPIPrepayResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析预支付响应失败: %w", err)
	}
	if result.PrepayID == "" {
		return nil, fmt.Errorf("微信未返回 prepay_id")
	}
	return &result, nil
}

// CloseOrder 关闭未支付的订单（超时关单用）。
func (c *Client) CloseOrder(mchID, outTradeNo string) error {
	body, _ := json.Marshal(map[string]string{"mchid": mchID})
	path := fmt.Sprintf("/v3/pay/transactions/out-trade-no/%s/close", outTradeNo)
	_, _, err := c.Do("POST", path, body)
	if isNotFound(err) {
		// 订单不存在或已支付，视为已关闭
		return nil
	}
	return err
}

// QueryOrder 按商户订单号查询微信支付订单状态。
func (c *Client) QueryOrder(mchID, outTradeNo string) (*QueryOrderResponse, error) {
	path := fmt.Sprintf("/v3/pay/transactions/out-trade-no/%s?mchid=%s", outTradeNo, mchID)
	_, respBody, err := c.Do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var result QueryOrderResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析订单查询响应失败: %w", err)
	}
	return &result, nil
}

// QueryOrderResponse 订单查询响应。
type QueryOrderResponse struct {
	AppID         string `json:"appid"`
	MchID         string `json:"mchid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"` // SUCCESS/REFUND/NOTPAY/CLOSED/...
	TradeStateDesc string `json:"trade_state_desc"`
	Amount        struct {
		Total         int    `json:"total"`
		PayerTotal    int    `json:"payer_total"`
		Currency      string `json:"currency"`
		PayerCurrency string `json:"payer_currency"`
	} `json:"amount"`
}
