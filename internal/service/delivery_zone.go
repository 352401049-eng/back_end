package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/geo"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrDeliveryZoneNotFound        = errors.New("delivery zone not found")
	ErrDeliveryZoneInvalid         = errors.New("delivery zone invalid")
	ErrDeliveryOutOfRange          = errors.New("delivery out of range")
	ErrDeliveryCoordinatesRequired = errors.New("delivery coordinates required")
)

type DeliveryZoneService struct {
	DB *gorm.DB
}

type DeliveryZoneView struct {
	MerchantID uint64           `json:"merchant_id"`
	Enabled    uint8            `json:"enabled"`
	Points     []model.GeoPoint `json:"points"`
}

type DeliveryZoneCheckResult struct {
	InZone      bool `json:"in_zone"`
	ZoneEnabled bool `json:"zone_enabled"`
}

type UpsertDeliveryZoneInput struct {
	Enabled *uint8
	Points  []model.GeoPoint
}

func (s *DeliveryZoneService) GetByMerchantID(merchantID uint64) (*model.MerchantDeliveryZone, error) {
	var zone model.MerchantDeliveryZone
	if err := query.NotDeleted(s.DB).Where("merchant_id = ?", merchantID).First(&zone).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryZoneNotFound
		}
		return nil, err
	}
	return &zone, nil
}

func (s *DeliveryZoneService) ToView(zone *model.MerchantDeliveryZone) DeliveryZoneView {
	if zone == nil {
		return DeliveryZoneView{}
	}
	points := zone.Points
	if points == nil {
		points = []model.GeoPoint{}
	}
	return DeliveryZoneView{
		MerchantID: zone.MerchantID,
		Enabled:    zone.Enabled,
		Points:     points,
	}
}

func (s *DeliveryZoneService) GetView(merchantID uint64) (*DeliveryZoneView, error) {
	zone, err := s.GetByMerchantID(merchantID)
	if err != nil {
		if errors.Is(err, ErrDeliveryZoneNotFound) {
			return nil, nil
		}
		return nil, err
	}
	view := s.ToView(zone)
	return &view, nil
}

func (s *DeliveryZoneService) GetPublicView(merchantID uint64) (*DeliveryZoneView, error) {
	zone, err := s.GetByMerchantID(merchantID)
	if err != nil {
		if errors.Is(err, ErrDeliveryZoneNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if zone.Enabled != model.DeliveryZoneEnabled {
		return nil, nil
	}
	view := s.ToView(zone)
	return &view, nil
}

func (s *DeliveryZoneService) Upsert(merchantID uint64, input UpsertDeliveryZoneInput) (*DeliveryZoneView, error) {
	enabled := model.DeliveryZoneEnabled
	if input.Enabled != nil {
		if *input.Enabled != model.DeliveryZoneDisabled && *input.Enabled != model.DeliveryZoneEnabled {
			return nil, fmt.Errorf("%w: enabled 须为 0 或 1", ErrDeliveryZoneInvalid)
		}
		enabled = *input.Enabled
	}
	if enabled == model.DeliveryZoneEnabled {
		if err := geo.ValidatePolygonPoints(input.Points); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
		}
	} else if len(input.Points) > 0 {
		if err := geo.ValidatePolygonPoints(input.Points); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
		}
	} else {
		input.Points = []model.GeoPoint{}
	}

	existing, err := s.GetByMerchantID(merchantID)
	if err != nil && !errors.Is(err, ErrDeliveryZoneNotFound) {
		return nil, err
	}

	points := input.Points
	if points == nil {
		points = []model.GeoPoint{}
	}

	if existing == nil {
		zone := model.MerchantDeliveryZone{
			MerchantID: merchantID,
			Enabled:    enabled,
			Points:     points,
		}
		if enabled == model.DeliveryZoneEnabled && len(points) < 3 {
			return nil, fmt.Errorf("%w: 启用配送范围时至少需要 3 个顶点", ErrDeliveryZoneInvalid)
		}
		if err := s.DB.Create(&zone).Error; err != nil {
			return nil, fmt.Errorf("保存配送范围失败: %w", err)
		}
		view := s.ToView(&zone)
		return &view, nil
	}

	existing.Enabled = enabled
	existing.Points = points
	if err := s.DB.Save(existing).Error; err != nil {
		return nil, fmt.Errorf("更新配送范围失败: %w", err)
	}
	return s.GetView(merchantID)
}

func (s *DeliveryZoneService) Patch(merchantID uint64, input UpsertDeliveryZoneInput) (*DeliveryZoneView, error) {
	existing, err := s.GetByMerchantID(merchantID)
	if err != nil {
		if errors.Is(err, ErrDeliveryZoneNotFound) {
			if input.Enabled == nil {
				e := model.DeliveryZoneEnabled
				input.Enabled = &e
			}
			return s.Upsert(merchantID, input)
		}
		return nil, err
	}

	enabled := existing.Enabled
	if input.Enabled != nil {
		if *input.Enabled != model.DeliveryZoneDisabled && *input.Enabled != model.DeliveryZoneEnabled {
			return nil, fmt.Errorf("%w: enabled 须为 0 或 1", ErrDeliveryZoneInvalid)
		}
		enabled = *input.Enabled
	}

	points := existing.Points
	if input.Points != nil {
		points = input.Points
	}
	if enabled == model.DeliveryZoneEnabled {
		if err := geo.ValidatePolygonPoints(points); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
		}
	} else if len(points) > 0 {
		if err := geo.ValidatePolygonPoints(points); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
		}
	}

	existing.Enabled = enabled
	if input.Points != nil {
		existing.Points = points
	}
	if err := s.DB.Save(existing).Error; err != nil {
		return nil, fmt.Errorf("更新配送范围失败: %w", err)
	}
	return s.GetView(merchantID)
}

func (s *DeliveryZoneService) Delete(merchantID uint64) error {
	zone, err := s.GetByMerchantID(merchantID)
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, zone).Error
}

func (s *DeliveryZoneService) CheckPoint(merchantID uint64, lat, lng float64) (*DeliveryZoneCheckResult, error) {
	if err := geo.ValidateCoordinate(lat, lng); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
	}
	zone, err := s.GetByMerchantID(merchantID)
	if err != nil {
		if errors.Is(err, ErrDeliveryZoneNotFound) {
			return &DeliveryZoneCheckResult{InZone: true, ZoneEnabled: false}, nil
		}
		return nil, err
	}
	if zone.Enabled != model.DeliveryZoneEnabled || len(zone.Points) < 3 {
		return &DeliveryZoneCheckResult{InZone: true, ZoneEnabled: false}, nil
	}
	in := geo.PointInPolygon(lat, lng, zone.Points)
	return &DeliveryZoneCheckResult{InZone: in, ZoneEnabled: true}, nil
}

// ValidateDeliveryPoint 配送单校验坐标是否在商家配送范围内。
func (s *DeliveryZoneService) ValidateDeliveryPoint(merchantID uint64, deliveryType uint8, lat, lng *float64) error {
	if deliveryType != model.DeliveryTypeDelivery {
		return nil
	}
	zone, err := s.GetByMerchantID(merchantID)
	if err != nil {
		if errors.Is(err, ErrDeliveryZoneNotFound) {
			return nil
		}
		return err
	}
	if zone.Enabled != model.DeliveryZoneEnabled || len(zone.Points) < 3 {
		return nil
	}
	if lat == nil || lng == nil {
		return ErrDeliveryCoordinatesRequired
	}
	if err := geo.ValidateCoordinate(*lat, *lng); err != nil {
		return fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
	}
	if !geo.PointInPolygon(*lat, *lng, zone.Points) {
		return ErrDeliveryOutOfRange
	}
	return nil
}

// DeliveryCoordinateInput 解析配送坐标：请求体坐标 > 地址快照 > 收货地址 ID。
type DeliveryCoordinateInput struct {
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	AddressSnapshot   *model.AddressSnapshot
}

func (s *DeliveryZoneService) ResolveDeliveryCoordinates(accountID uint64, deliveryType uint8, in DeliveryCoordinateInput) (*float64, *float64, error) {
	// 用户选了 address_id 时，以地址库坐标为准，避免请求体坐标与所选地址不一致导致校验被绕过。
	if deliveryType == model.DeliveryTypeDelivery && in.AddressID != nil {
		var addr model.UserAddress
		if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", *in.AddressID, accountID).First(&addr).Error; err != nil {
			return nil, nil, ErrAddressRequired
		}
		return addr.Latitude, addr.Longitude, nil
	}
	if in.AddressSnapshot != nil && in.AddressSnapshot.Latitude != nil && in.AddressSnapshot.Longitude != nil {
		return in.AddressSnapshot.Latitude, in.AddressSnapshot.Longitude, nil
	}
	if in.DeliveryLatitude != nil && in.DeliveryLongitude != nil {
		return in.DeliveryLatitude, in.DeliveryLongitude, nil
	}
	if deliveryType != model.DeliveryTypeDelivery {
		return nil, nil, nil
	}
	return nil, nil, nil
}

func (s *DeliveryZoneService) ValidateDelivery(accountID, merchantID uint64, deliveryType uint8, in DeliveryCoordinateInput) error {
	lat, lng, err := s.ResolveDeliveryCoordinates(accountID, deliveryType, in)
	if err != nil {
		return err
	}
	return s.ValidateDeliveryPoint(merchantID, deliveryType, lat, lng)
}
