package payment

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrNotConfigured = errors.New("payment provider not configured")
	ErrNotSupported  = errors.New("payment operation not supported")
	ErrInvalidState  = errors.New("payment state invalid")
)

// Provider 支付渠道抽象。当前默认 Mock；微信实现为桩，便于后续替换。
type Provider interface {
	Name() string
	// ImmediateSettle 是否在下单事务内直接记已支付（Mock=true；微信=false）。
	ImmediateSettle() bool
	// SettlePaidInTx 在事务内将订单标记为已支付。
	SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error
	// RefundInTx 在事务内将已支付订单标记为已退款（Mock 立即成功；微信后续接真实退款）。
	RefundInTx(tx *gorm.DB, orderID uint64) error
	// CreatePrepay 为未支付订单创建预支付参数。Mock 若已付则返回已结算；微信桩返回 ErrNotConfigured。
	CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error)
	// HandleNotify 处理支付渠道异步回调。微信桩返回 ErrNotConfigured。
	HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error)
}

type PrepayResult struct {
	Provider    string `json:"provider"`
	AlreadyPaid bool   `json:"already_paid"`
	NeedPay     bool   `json:"need_pay"`
	// Params 预留给小程序 wx.requestPayment 的字段（微信支付启用后填充）。
	Params  map[string]interface{} `json:"params,omitempty"`
	Message string                 `json:"message,omitempty"`
}

type NotifyResult struct {
	OrderID uint64 `json:"order_id"`
	OrderNo string `json:"order_no"`
	Paid    bool   `json:"paid"`
	RawAck  string `json:"raw_ack,omitempty"`
}
