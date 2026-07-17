package model

import "time"

const (
	AnnouncementStatusHidden   uint8 = 0
	AnnouncementStatusPublished uint8 = 1
)

type Announcement struct {
	ID         uint64     `gorm:"primaryKey" json:"id"`
	MerchantID uint64     `gorm:"not null;default:0" json:"merchant_id"`
	Title      string     `gorm:"size:128;not null" json:"title"`
	Content    string     `gorm:"type:text;not null" json:"content"`
	CoverURL   *string    `gorm:"column:cover_url;size:512" json:"cover_url,omitempty"`
	SortOrder  int        `gorm:"not null;default:0" json:"sort_order"`
	Status     uint8      `gorm:"not null;default:1" json:"status"`
	PublishAt  *time.Time `json:"publish_at,omitempty"`
	ExpireAt   *time.Time `json:"expire_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	SoftDelete
}

func (Announcement) TableName() string { return "announcement" }
