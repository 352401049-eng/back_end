package model

import "time"

const (
	DeliveryZoneDisabled uint8 = 0
	DeliveryZoneEnabled  uint8 = 1
)

// GeoPoint 经纬度点（纬度、经度）。
type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type MerchantDeliveryZone struct {
	ID         uint64     `gorm:"primaryKey" json:"id"`
	MerchantID uint64     `gorm:"not null;uniqueIndex" json:"merchant_id"`
	Enabled    uint8      `gorm:"not null;default:1" json:"enabled"`
	Points     []GeoPoint `gorm:"serializer:json;not null" json:"points"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	SoftDelete
}

func (MerchantDeliveryZone) TableName() string { return "merchant_delivery_zone" }
