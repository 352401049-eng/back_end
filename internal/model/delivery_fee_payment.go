package model

import (
	"encoding/json"
	"time"
)

const (
	DeliveryFeeStatusPendingPay uint8 = 0
	DeliveryFeeStatusFulfilled  uint8 = 1
	DeliveryFeeStatusCancelled  uint8 = 8
)

// DeliveryFeeOrder 背包跑腿配送费预支付单；支付成功后再执行扣包与建配送单。
type DeliveryFeeOrder struct {
	ID               uint64          `gorm:"primaryKey" json:"id"`
	OrderNo          string          `gorm:"size:32;not null" json:"order_no"`
	AccountID        uint64          `gorm:"not null" json:"account_id"`
	MerchantID       uint64          `gorm:"not null" json:"merchant_id"`
	Status           uint8           `gorm:"not null;default:0" json:"status"`
	Amount           float64         `gorm:"type:decimal(10,2);not null;default:0" json:"amount"`
	RiderEarnings    float64         `gorm:"type:decimal(10,2);not null;default:0" json:"rider_earnings"`
	PayAmount        float64         `gorm:"type:decimal(10,2);not null" json:"pay_amount"`
	PayStatus        uint8           `gorm:"not null;default:0" json:"pay_status"`
	PaidAt           *time.Time      `json:"paid_at,omitempty"`
	PayExpireAt      *time.Time      `gorm:"column:pay_expire_at" json:"pay_expire_at,omitempty"`
	RefundedAmount   float64         `gorm:"type:decimal(10,2);not null;default:0" json:"refunded_amount"`
	Payload          json.RawMessage `gorm:"type:json" json:"payload,omitempty"`
	DeliveryOrderID  *uint64         `json:"delivery_order_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	SoftDelete
}

func (DeliveryFeeOrder) TableName() string { return "delivery_fee_order" }
