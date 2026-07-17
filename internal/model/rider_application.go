package model

import "time"

const (
	RiderApplicationPending  = 0
	RiderApplicationApproved = 1
	RiderApplicationRejected = 2
)

type RiderApplication struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	AccountID    uint64     `gorm:"not null" json:"account_id"`
	RealName     string     `gorm:"size:32;not null" json:"real_name"`
	IDCardNo     *string    `gorm:"column:id_card_no;size:32" json:"id_card_no,omitempty"`
	Phone        string     `gorm:"size:20;not null" json:"phone"`
	Status       uint8      `gorm:"not null;default:0" json:"status"`
	ReviewerID   *uint64    `gorm:"column:reviewer_id" json:"reviewer_id,omitempty"`
	ReviewedAt   *time.Time `gorm:"column:reviewed_at" json:"reviewed_at,omitempty"`
	RejectReason *string    `gorm:"size:256" json:"reject_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SoftDelete
	Account      *Account   `gorm:"foreignKey:AccountID" json:"account,omitempty"`
}

func (RiderApplication) TableName() string { return "rider_application" }

func RiderApplicationStatusText(status uint8) string {
	switch status {
	case RiderApplicationPending:
		return "待审核"
	case RiderApplicationApproved:
		return "已通过"
	case RiderApplicationRejected:
		return "已拒绝"
	default:
		return "未知"
	}
}
