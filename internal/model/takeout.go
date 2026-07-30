package model

import (
	"encoding/json"
	"time"
)

const (
	TakeoutStatusPendingPay   uint8 = 0
	TakeoutStatusPreparing    uint8 = 1 // 配餐中
	TakeoutStatusFulfilling   uint8 = 2 // 已出餐/配送中
	TakeoutStatusCompleted    uint8 = 3
	TakeoutStatusCancelled    uint8 = 8
)

const (
	PaySubjectOrder       = "order"
	PaySubjectTakeout     = "takeout"
	PaySubjectDeliveryFee = "delivery_fee"
)

type TakeoutOrder struct {
	ID                 uint64           `gorm:"primaryKey" json:"id"`
	OrderNo            string           `gorm:"size:32;not null" json:"order_no"`
	AccountID          uint64           `gorm:"not null" json:"account_id"`
	MerchantID         uint64           `gorm:"not null" json:"merchant_id"`
	Status             uint8            `gorm:"not null;default:0" json:"status"`
	GoodsAmount        float64          `gorm:"type:decimal(10,2);not null" json:"goods_amount"`
	DeliveryFee        float64          `gorm:"type:decimal(10,2);not null;default:0" json:"delivery_fee"`
	RiderEarnings      float64          `gorm:"type:decimal(10,2);not null;default:0" json:"rider_earnings"`
	PayAmount          float64          `gorm:"type:decimal(10,2);not null" json:"pay_amount"`
	PayStatus          uint8            `gorm:"not null;default:0" json:"pay_status"`
	PaidAt             *time.Time       `json:"paid_at,omitempty"`
	PayExpireAt        *time.Time       `gorm:"column:pay_expire_at" json:"pay_expire_at,omitempty"`
	RefundedAmount     float64          `gorm:"type:decimal(10,2);not null;default:0" json:"refunded_amount"`
	AddressSnapshot    *AddressSnapshot `gorm:"type:json" json:"address_snapshot,omitempty"`
	DeliveryTimeRemark string           `gorm:"size:128;not null;default:''" json:"delivery_time_remark"`
	PackageSelections  json.RawMessage  `gorm:"type:json" json:"package_selections,omitempty"`
	OptionSelections   json.RawMessage  `gorm:"type:json" json:"option_selections,omitempty"`
	DeliveryOrderID    *uint64          `json:"delivery_order_id,omitempty"`
	Remark             *string          `gorm:"size:512" json:"remark,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	SoftDelete
	Items []TakeoutOrderItem `gorm:"foreignKey:TakeoutOrderID" json:"items,omitempty"`
}

func (TakeoutOrder) TableName() string { return "takeout_order" }

type TakeoutOrderItem struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	TakeoutOrderID uint64    `gorm:"not null" json:"takeout_order_id"`
	ProductID      uint64    `gorm:"not null" json:"product_id"`
	ProductName    string    `gorm:"size:128;not null" json:"product_name"`
	ProductImage   *string   `gorm:"size:512" json:"product_image,omitempty"`
	UnitPrice      float64   `gorm:"type:decimal(10,2);not null" json:"unit_price"`
	Quantity       uint32    `gorm:"not null" json:"quantity"`
	Subtotal       float64   `gorm:"type:decimal(10,2);not null" json:"subtotal"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	SoftDelete
}

func (TakeoutOrderItem) TableName() string { return "takeout_order_item" }
