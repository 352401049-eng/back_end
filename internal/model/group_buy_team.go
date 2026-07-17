package model

import "time"

const (
	GroupBuyTeamPending  uint8 = 0
	GroupBuyTeamSuccess  uint8 = 1
	GroupBuyTeamFailed   uint8 = 2
)

type GroupBuyTeam struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	GroupBuyID   uint64     `gorm:"not null" json:"group_buy_id"`
	LeaderID     uint64     `gorm:"not null" json:"leader_id"`
	TargetCount  uint32     `gorm:"not null" json:"target_count"`
	CurrentCount uint32     `gorm:"not null;default:0" json:"current_count"`
	Status       uint8      `gorm:"not null;default:0" json:"status"`
	ExpireAt     time.Time  `gorm:"not null" json:"expire_at"`
	SuccessAt    *time.Time `json:"success_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SoftDelete
}

func (GroupBuyTeam) TableName() string { return "group_buy_team" }

type GroupBuyMember struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TeamID    uint64    `gorm:"not null" json:"team_id"`
	OrderID   uint64    `gorm:"not null" json:"order_id"`
	AccountID uint64    `gorm:"not null" json:"account_id"`
	IsLeader  uint8     `gorm:"not null;default:0" json:"is_leader"`
	JoinedAt  time.Time `json:"joined_at"`
	SoftDelete
}

func (GroupBuyMember) TableName() string { return "group_buy_member" }
