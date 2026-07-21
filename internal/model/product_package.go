package model

import "time"

// ProductPackageGroup 套餐分组（组内 x 选 y）。
type ProductPackageGroup struct {
	ID                uint64               `gorm:"primaryKey" json:"id"`
	PackageProductID  uint64               `gorm:"not null;index" json:"package_product_id"`
	Name              string               `gorm:"size:64;not null" json:"name"`
	SelectCount       uint32               `gorm:"not null;default:1" json:"select_count"`
	SortOrder         int                  `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	SoftDelete
	Items             []ProductPackageItem `gorm:"foreignKey:GroupID" json:"items,omitempty"`
}

func (ProductPackageGroup) TableName() string { return "product_package_group" }

// ProductPackageItem 分组内候选商品。
type ProductPackageItem struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	GroupID    uint64    `gorm:"not null;index" json:"group_id"`
	ProductID  uint64    `gorm:"not null;index" json:"product_id"`
	MerchantID uint64    `gorm:"not null;default:0" json:"merchant_id"`
	MaxQty     uint32    `gorm:"not null;default:1" json:"max_qty"`
	SortOrder  int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	SoftDelete
	Product    *Product  `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (ProductPackageItem) TableName() string { return "product_package_item" }
