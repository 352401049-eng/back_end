package model

import "time"

const (
	InventoryEventOrderCredit   = "order_credit"
	InventoryEventOrderRollback = "order_rollback"
	InventoryEventUse           = "use"
	InventoryEventUseCancel     = "use_cancel"
)

const (
	InventoryUsagePendingVerify  uint8 = 1
	InventoryUsagePendingShip    uint8 = 2
	InventoryUsageCompleted      uint8 = 3
	InventoryUsageCancelled      uint8 = 4
	InventoryUsageCancelPending  uint8 = 5
)

type UserInventoryLog struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	AccountID   uint64    `gorm:"not null" json:"account_id"`
	InventoryID *uint64   `gorm:"column:inventory_id" json:"inventory_id,omitempty"`
	ProductID   uint64    `gorm:"not null" json:"product_id"`
	Spec        string    `gorm:"size:128;not null;default:''" json:"spec"`
	OrderID     *uint64   `gorm:"column:order_id" json:"order_id,omitempty"`
	UsageID     *uint64   `gorm:"column:usage_id" json:"usage_id,omitempty"`
	EventType   string    `gorm:"size:32;not null" json:"event_type"`
	DeltaQty    int32     `gorm:"not null" json:"delta_qty"`
	BeforeQty   uint32    `gorm:"not null;default:0" json:"before_qty"`
	AfterQty    uint32    `gorm:"not null;default:0" json:"after_qty"`
	Remark      *string   `gorm:"size:256" json:"remark,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	SoftDelete
}

func (UserInventoryLog) TableName() string { return "user_inventory_log" }

type UserInventoryUsage struct {
	ID              uint64           `gorm:"primaryKey" json:"id"`
	AccountID       uint64           `gorm:"not null" json:"account_id"`
	InventoryID     uint64           `gorm:"not null" json:"inventory_id"`
	ProductID       uint64           `gorm:"not null" json:"product_id"`
	MerchantID      uint64           `gorm:"not null" json:"merchant_id"`
	SourceOrderID   *uint64          `gorm:"column:source_order_id" json:"source_order_id,omitempty"`
	Quantity        uint32           `gorm:"not null" json:"quantity"`
	DeliveryType    uint8            `gorm:"not null" json:"delivery_type"`
	AddressSnapshot *AddressSnapshot `gorm:"type:json" json:"address_snapshot,omitempty"`
	Status          uint8            `gorm:"not null;default:1" json:"status"`
	DeliveryOrderID *uint64          `gorm:"column:delivery_order_id" json:"delivery_order_id,omitempty"`
	CancelReason    *string          `gorm:"size:256" json:"cancel_reason,omitempty"`
	Remark          *string          `gorm:"size:256" json:"remark,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	SoftDelete
	Product       *Product        `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Inventory     *UserInventory  `gorm:"foreignKey:InventoryID" json:"inventory,omitempty"`
	DeliveryOrder *DeliveryOrder  `gorm:"foreignKey:DeliveryOrderID" json:"delivery_order,omitempty"`
}

func (UserInventoryUsage) TableName() string { return "user_inventory_usage" }

func InventoryUsageStatusText(status uint8) string {
	switch status {
	case InventoryUsagePendingVerify:
		return "待核销"
	case InventoryUsagePendingShip:
		return "待发货"
	case InventoryUsageCompleted:
		return "已完成"
	case InventoryUsageCancelled:
		return "已取消"
	case InventoryUsageCancelPending:
		return "取消待审核"
	default:
		return "未知"
	}
}
