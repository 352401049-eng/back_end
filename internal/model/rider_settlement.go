package model

import "time"

const (
	RiderSettlementPending  uint8 = 0 // 待审批
	RiderSettlementApproved uint8 = 1 // 通过
	RiderSettlementRejected uint8 = 2 // 拒绝

	RiderSettlementSourceRider uint8 = 0 // 骑手申请
	RiderSettlementSourceAdmin uint8 = 1 // 管理员主动
)

type RiderSettlement struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	RiderID      uint64     `gorm:"not null;index:idx_rider_status,priority:1" json:"rider_id"`
	Amount       float64    `gorm:"type:decimal(10,2);not null" json:"amount"`
	Status       uint8      `gorm:"not null;default:0;index:idx_rider_status,priority:2;index:idx_status" json:"status"`
	Source       uint8      `gorm:"not null;default:0" json:"source"`
	OperatorID   *uint64    `json:"operator_id,omitempty"`
	ApplicantID  *uint64    `json:"applicant_id,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	RejectReason *string    `gorm:"size:256" json:"reject_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SoftDelete
}

func (RiderSettlement) TableName() string { return "rider_settlement" }
