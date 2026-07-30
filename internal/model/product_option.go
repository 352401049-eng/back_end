package model

import "time"

type ProductOptionGroup struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	ProductID uint64    `gorm:"not null" json:"product_id"`
	Title     string    `gorm:"size:64;not null" json:"title"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SoftDelete
	Items []ProductOptionItem `gorm:"foreignKey:GroupID" json:"items,omitempty"`
}

func (ProductOptionGroup) TableName() string { return "product_option_group" }

type ProductOptionItem struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	GroupID   uint64    `gorm:"not null" json:"group_id"`
	Label     string    `gorm:"size:64;not null" json:"label"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SoftDelete
}

func (ProductOptionItem) TableName() string { return "product_option_item" }

const (
	OptionSelectNone    uint8 = 0
	OptionSelectPending uint8 = 1
	OptionSelectDone    uint8 = 2
)

type OptionSelectionSnapshot []OptionSelectionUnitSnap

type OptionSelectionUnitSnap struct {
	UnitIndex   uint32                     `json:"unit_index"`
	ProductID   uint64                     `json:"product_id"`
	ProductName string                     `json:"product_name,omitempty"`
	Groups      []OptionSelectionGroupSnap `json:"groups"`
}

type OptionSelectionGroupSnap struct {
	GroupID     uint64 `json:"group_id"`
	GroupTitle  string `json:"group_title,omitempty"`
	OptionID    uint64 `json:"option_id"`
	OptionLabel string `json:"option_label,omitempty"`
}
