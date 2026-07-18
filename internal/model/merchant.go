package model

import "time"

const (
	MerchantStatusClosed = 0
	MerchantStatusOpen   = 1

	ProductStatusOff = 0
	ProductStatusOn  = 1
)

type MerchantProfile struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	AccountID    uint64    `gorm:"not null;uniqueIndex" json:"account_id"`
	ShopName     string    `gorm:"size:128;not null" json:"shop_name"`
	ShopLogo     *string   `gorm:"column:shop_logo;size:512" json:"shop_logo,omitempty"`
	Images       []string  `gorm:"serializer:json" json:"images,omitempty"`
	ContactPhone *string   `gorm:"size:20" json:"contact_phone,omitempty"`
	Address      *string   `gorm:"size:256" json:"address,omitempty"`
	Latitude     *float64  `gorm:"type:decimal(10,7)" json:"latitude"`
	Longitude    *float64  `gorm:"type:decimal(10,7)" json:"longitude"`
	Status       uint8     `gorm:"not null;default:1" json:"status"`
	AllowReservation uint8 `gorm:"not null;default:0" json:"allow_reservation"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	SoftDelete
	Account      *Account  `gorm:"foreignKey:AccountID" json:"account,omitempty"`
}

func (MerchantProfile) TableName() string { return "merchant_profile" }

type ProductCategory struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	MerchantID uint64    `gorm:"not null;default:0" json:"merchant_id"`
	ParentID   uint64    `gorm:"not null;default:0" json:"parent_id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	IconURL   *string   `gorm:"column:icon_url;size:512" json:"icon_url,omitempty"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	Status    uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SoftDelete
}

func (ProductCategory) TableName() string { return "product_category" }
