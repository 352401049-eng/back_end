package model

import "time"

type UserProfile struct {
	ID        uint64     `gorm:"primaryKey" json:"id"`
	AccountID uint64     `gorm:"not null;uniqueIndex" json:"account_id"`
	RealName  *string    `gorm:"size:32" json:"real_name,omitempty"`
	Birthday  *time.Time `gorm:"type:date" json:"birthday,omitempty"`
	Bio       *string    `gorm:"size:256" json:"bio,omitempty"`
	Province  *string    `gorm:"size:32" json:"province,omitempty"`
	City      *string    `gorm:"size:32" json:"city,omitempty"`
	District  *string    `gorm:"size:32" json:"district,omitempty"`
	Points    uint32     `gorm:"not null;default:0" json:"points"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	SoftDelete
}

func (UserProfile) TableName() string { return "user_profile" }

type UserAddress struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	AccountID    uint64    `gorm:"not null" json:"account_id"`
	ContactName  string    `gorm:"size:32;not null" json:"contact_name"`
	ContactPhone string    `gorm:"size:20;not null" json:"contact_phone"`
	Province     string    `gorm:"size:32;not null" json:"province"`
	City         string    `gorm:"size:32;not null" json:"city"`
	District     string    `gorm:"size:32;not null" json:"district"`
	Detail       string    `gorm:"size:256;not null" json:"detail"`
	Latitude     *float64  `gorm:"type:decimal(10,7)" json:"latitude"`
	Longitude    *float64  `gorm:"type:decimal(10,7)" json:"longitude"`
	LocationName *string   `gorm:"size:128" json:"location_name"`
	IsDefault    uint8     `gorm:"not null;default:0" json:"is_default"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	SoftDelete
}

func (UserAddress) TableName() string { return "user_address" }
