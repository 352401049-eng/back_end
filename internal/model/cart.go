package model

import "time"

const (
	PurchaseTypeSolo  uint8 = 1
	PurchaseTypeGroup uint8 = 2
)

type CartItem struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	AccountID      uint64    `gorm:"not null" json:"account_id"`
	ProductID      uint64    `gorm:"not null" json:"product_id"`
	PurchaseType   uint8     `gorm:"not null;default:1" json:"purchase_type"`
	GroupBuyID     *uint64   `gorm:"column:group_buy_id" json:"group_buy_id,omitempty"`
	GroupBuyTeamID *uint64   `gorm:"column:group_buy_team_id" json:"group_buy_team_id,omitempty"`
	Spec           *string   `gorm:"size:128" json:"spec,omitempty"`
	Quantity       uint32    `gorm:"not null;default:1" json:"quantity"`
	Selected       uint8     `gorm:"not null;default:1" json:"selected"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	SoftDelete
	Product        Product   `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (CartItem) TableName() string { return "cart_item" }

type GroupBuy struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	ProductID   uint64    `gorm:"not null" json:"product_id"`
	TargetCount uint32    `gorm:"not null" json:"target_count"`
	GroupPrice  float64   `gorm:"type:decimal(10,2);not null" json:"group_price"`
	StartAt     time.Time `json:"start_at"`
	EndAt       time.Time `json:"end_at"`
	Status      uint8     `gorm:"not null;default:1" json:"status"`
	SoftDelete
}

func (GroupBuy) TableName() string { return "group_buy" }
