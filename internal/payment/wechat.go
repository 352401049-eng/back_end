package payment

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// WeChatProvider 微信支付桩：预留下单预支付与回调入口，当前未配置商户号时一律失败。
// 后续接入时在此实现统一下单、签名校验、回调幂等入账。
type WeChatProvider struct {
	DB        *gorm.DB
	AppID     string
	MchID     string
	APIKey    string // v2 或后续换成证书路径
	NotifyURL string
	Enabled   bool
}

func (p *WeChatProvider) Name() string          { return "wechat" }
func (p *WeChatProvider) ImmediateSettle() bool { return false }

func (p *WeChatProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	// 微信渠道禁止业务层直接“记已付”，必须经回调/查单确认。
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

func (p *WeChatProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	if !p.Enabled {
		return ErrNotConfigured
	}
	// TODO: 调用微信退款 API，成功后再改 pay_status
	return fmt.Errorf("%w: wechat refund not implemented", ErrNotConfigured)
}

func (p *WeChatProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	if !p.Enabled || p.MchID == "" || p.APIKey == "" {
		return nil, ErrNotConfigured
	}
	// TODO: 调用微信统一下单，返回 timeStamp/nonceStr/package/signType/paySign
	return nil, fmt.Errorf("%w: wechat prepay not implemented", ErrNotConfigured)
}

func (p *WeChatProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	if !p.Enabled {
		return nil, ErrNotConfigured
	}
	// TODO: 验签 → 解析 out_trade_no → 幂等 SettlePaid
	return nil, fmt.Errorf("%w: wechat notify not implemented", ErrNotConfigured)
}
