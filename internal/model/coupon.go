package model

import "time"

const (
	CouponTypeFixed = 1 // 满减
	CouponTypeRate  = 2 // 折扣

	CouponScopeAll      uint8 = 0
	CouponScopeCategory uint8 = 1
	CouponScopeProduct  uint8 = 2

	CouponStatusDisabled uint8 = 0
	CouponStatusEnabled  uint8 = 1

	UserCouponStatusUnused  uint8 = 0
	UserCouponStatusUsed    uint8 = 1
	UserCouponStatusExpired uint8 = 2
)

type Coupon struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"size:64;not null" json:"name"`
	Type           uint8     `gorm:"not null" json:"type"`
	MerchantID     *uint64   `gorm:"column:merchant_id" json:"merchant_id,omitempty"`
	MinAmount      float64   `gorm:"type:decimal(10,2);not null;default:0" json:"min_amount"`
	DiscountAmount *float64  `gorm:"type:decimal(10,2)" json:"discount_amount,omitempty"`
	DiscountRate   *uint8    `json:"discount_rate,omitempty"`
	MaxDiscount    *float64  `gorm:"type:decimal(10,2)" json:"max_discount,omitempty"`
	TotalQuota     uint32    `gorm:"not null;default:0" json:"total_quota"`
	ReceivedCount  uint32    `gorm:"not null;default:0" json:"received_count"`
	ScopeType      uint8     `gorm:"not null;default:0" json:"scope_type"`
	ScopeIDs       []uint64  `gorm:"serializer:json" json:"scope_ids,omitempty"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	Status         uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	SoftDelete
}

func (Coupon) TableName() string { return "coupon" }

type UserCoupon struct {
	ID         uint64     `gorm:"primaryKey" json:"id"`
	AccountID  uint64     `gorm:"not null" json:"account_id"`
	CouponID   uint64     `gorm:"not null" json:"coupon_id"`
	Status     uint8      `gorm:"not null;default:0" json:"status"`
	OrderID    *uint64    `json:"order_id,omitempty"`
	ReceivedAt time.Time  `json:"received_at"`
	UsedAt     *time.Time `json:"used_at,omitempty"`
	ExpiredAt  time.Time  `json:"expired_at"`
	SoftDelete
	Coupon Coupon `gorm:"foreignKey:CouponID" json:"coupon,omitempty"`
}

func (UserCoupon) TableName() string { return "user_coupon" }
