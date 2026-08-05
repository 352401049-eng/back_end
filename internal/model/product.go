package model

import "time"

const (
	ProductItemTypePhysical = 1
	ProductItemTypeVirtual  = 2
	ProductItemTypePackage  = 3 // 平台套餐（跨店组合）
)

type Product struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	MerchantID     uint64    `gorm:"not null" json:"merchant_id"`
	CategoryID     uint64    `gorm:"not null" json:"category_id"`
	Name           string    `gorm:"size:128;not null" json:"name"`
	Description    *string   `gorm:"type:text" json:"description,omitempty"`
	CoverURL       string    `gorm:"column:cover_url;size:512;not null" json:"cover_url"`
	Images         []string  `gorm:"serializer:json" json:"images,omitempty"`
	Price          float64   `gorm:"type:decimal(10,2);not null" json:"price"`
	OriginalPrice  *float64  `gorm:"column:original_price;type:decimal(10,2)" json:"original_price,omitempty"`
	Stock          uint32    `gorm:"not null;default:0" json:"stock"`
	EnableDeal     uint8     `gorm:"column:enable_deal;not null;default:1" json:"enable_deal"`
	EnableGroup    uint8     `gorm:"column:enable_group;not null;default:0" json:"enable_group"`
	EnableTakeout  uint8     `gorm:"column:enable_takeout;not null;default:0" json:"enable_takeout"`
	DealStock      uint32    `gorm:"column:deal_stock;not null;default:0" json:"deal_stock"`
	GroupStock     uint32    `gorm:"column:group_stock;not null;default:0" json:"group_stock"`
	TakeoutStock   uint32    `gorm:"column:takeout_stock;not null;default:0" json:"takeout_stock"`
	SalesCount     uint32    `gorm:"not null;default:0" json:"sales_count"`
	IsHot               uint8     `gorm:"not null;default:0" json:"is_hot"`
	EnableGroupBuy      uint8     `gorm:"not null;default:0" json:"enable_group_buy"`
	EnableCoupon        uint8     `gorm:"not null;default:1" json:"enable_coupon"`
	AllowPickup         uint8     `gorm:"not null;default:1" json:"allow_pickup"`
	AllowDelivery       uint8     `gorm:"not null;default:1" json:"allow_delivery"`
	GroupBuyTargetCount *uint32   `gorm:"column:group_buy_target_count" json:"group_buy_target_count,omitempty"`
	GroupBuyPrice       *float64  `gorm:"column:group_buy_price;type:decimal(10,2)" json:"group_buy_price,omitempty"`
	GroupBuyAllowRepeat uint8     `gorm:"column:group_buy_allow_repeat;not null;default:0" json:"group_buy_allow_repeat"`
	GroupBuyMaxConcurrentTeams uint32 `gorm:"column:group_buy_max_concurrent_teams;not null;default:0" json:"group_buy_max_concurrent_teams"`
	DealExpireDays      *uint32   `gorm:"column:deal_expire_days" json:"deal_expire_days,omitempty"`
	GroupExpireDays     *uint32   `gorm:"column:group_expire_days" json:"group_expire_days,omitempty"`
	ItemType            uint8     `gorm:"not null;default:1" json:"item_type"`
	Status         uint8     `gorm:"not null;default:1" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	SoftDelete
	Category       *ProductCategory `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Merchant       *MerchantProfile `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
}

func (Product) TableName() string { return "product" }

type UserInventory struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	AccountID   uint64    `gorm:"not null" json:"account_id"`
	ProductID   uint64    `gorm:"not null" json:"product_id"`
	Spec        string    `gorm:"size:128;not null;default:''" json:"spec"`
	Quantity    uint32    `gorm:"not null;default:0" json:"quantity"`
	LastOrderID *uint64   `gorm:"column:last_order_id" json:"last_order_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	SoftDelete
	Product     Product   `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (UserInventory) TableName() string { return "user_inventory" }
