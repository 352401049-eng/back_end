package model

import "time"

const (
	DeliveryZoneDisabled uint8 = 0
	DeliveryZoneEnabled  uint8 = 1

	DeliveryZoneModePolygon = "polygon"
	DeliveryZoneModeSpots   = "spots"

	DeliverySpotRadiusMinM uint32 = 100
	DeliverySpotRadiusMaxM uint32 = 2000
	DeliverySpotDefaultM   uint32 = 300
	DeliverySpotMaxCount   int    = 20
)

// GeoPoint 经纬度点（纬度、经度）。
type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// DeliverySpot 配送点 + 半径围栏（米）。
type DeliverySpot struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	RadiusM   uint32  `json:"radius_m"`
}

type MerchantDeliveryZone struct {
	ID         uint64         `gorm:"primaryKey" json:"id"`
	MerchantID uint64         `gorm:"not null;uniqueIndex" json:"merchant_id"`
	Enabled    uint8          `gorm:"not null;default:1" json:"enabled"`
	Mode       string         `gorm:"size:16;not null;default:polygon" json:"mode"`
	Points     []GeoPoint     `gorm:"serializer:json;not null" json:"points"`
	Spots      []DeliverySpot `gorm:"serializer:json" json:"spots"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	SoftDelete
}

func (MerchantDeliveryZone) TableName() string { return "merchant_delivery_zone" }

func NormalizeDeliveryZoneMode(mode string) string {
	if mode == DeliveryZoneModeSpots {
		return DeliveryZoneModeSpots
	}
	return DeliveryZoneModePolygon
}
