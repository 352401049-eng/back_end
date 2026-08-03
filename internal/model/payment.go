package model

import "time"

// 支付流水状态
const (
	PayTxStatusPrepay   uint8 = 0 // 预支付（已创建 prepay_id，等待支付）
	PayTxStatusPaid     uint8 = 1 // 已支付
	PayTxStatusRefunded uint8 = 2 // 已退款
	PayTxStatusFailed   uint8 = 3 // 支付失败/已关闭
)

// PaymentTransaction 支付流水。独立于 order 记录微信侧信息，用于对账和幂等。
type PaymentTransaction struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	SubjectType   string    `gorm:"size:32;not null;default:order" json:"subject_type"`
	SubjectID     uint64    `gorm:"not null;default:0" json:"subject_id"`
	OrderID       uint64    `gorm:"not null" json:"order_id"`
	OrderNo       string    `gorm:"size:32;not null" json:"order_no"`
	PrepayID      *string   `gorm:"size:64;uniqueIndex" json:"prepay_id,omitempty"`
	TransactionID *string   `gorm:"size:64;uniqueIndex" json:"transaction_id,omitempty"`
	PayAmount     float64   `gorm:"type:decimal(10,2);not null" json:"pay_amount"`
	Status        uint8     `gorm:"not null;default:0" json:"status"`
	WechatRaw     *string   `gorm:"type:json" json:"wechat_raw,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (PaymentTransaction) TableName() string { return "payment_transaction" }

// PaymentRefund 退款回调幂等记录（按微信 refund_id / out_refund_no 去重）。
type PaymentRefund struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	OrderNo      string    `gorm:"size:32;not null;index" json:"order_no"`
	OutRefundNo  string    `gorm:"size:64;not null" json:"out_refund_no"`
	RefundID     string    `gorm:"size:64;not null" json:"refund_id"`
	SubjectType  string    `gorm:"size:32;not null;default:order" json:"subject_type"`
	SubjectID    uint64    `gorm:"not null;default:0" json:"subject_id"`
	RefundAmount float64   `gorm:"type:decimal(10,2);not null" json:"refund_amount"`
	Status       uint8     `gorm:"not null;default:1" json:"status"` // 1=成功
	WechatRaw    *string   `gorm:"type:json" json:"wechat_raw,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (PaymentRefund) TableName() string { return "payment_refund" }
