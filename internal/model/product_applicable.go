package model

import "time"

type ProductApplicableMerchant struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	ProductID  uint64    `gorm:"not null;uniqueIndex:uk_product_merchant" json:"product_id"`
	MerchantID uint64    `gorm:"not null;uniqueIndex:uk_product_merchant;index:idx_merchant_product" json:"merchant_id"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ProductApplicableMerchant) TableName() string { return "product_applicable_merchant" }
