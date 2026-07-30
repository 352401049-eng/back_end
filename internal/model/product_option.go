package model

import (
	"fmt"
	"time"
)

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

// SummaryText 生成「可乐（糖度：少糖）；汉堡（辣度：微辣）」类摘要。
func (s OptionSelectionSnapshot) SummaryText() string {
	if len(s) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s))
	for _, u := range s {
		groupParts := make([]string, 0, len(u.Groups))
		for _, g := range u.Groups {
			title := g.GroupTitle
			label := g.OptionLabel
			if title != "" && label != "" {
				groupParts = append(groupParts, title+"："+label)
			} else if label != "" {
				groupParts = append(groupParts, label)
			} else if title != "" {
				groupParts = append(groupParts, title)
			}
		}
		if len(groupParts) == 0 {
			continue
		}
		groupText := groupParts[0]
		for i := 1; i < len(groupParts); i++ {
			groupText += " · " + groupParts[i]
		}
		name := u.ProductName
		if name == "" && u.ProductID > 0 {
			name = fmt.Sprintf("#%d", u.ProductID)
		}
		if name != "" {
			parts = append(parts, name+"（"+groupText+"）")
		} else {
			parts = append(parts, groupText)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "；" + parts[i]
	}
	return out
}
