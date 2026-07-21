package service

import (
	"errors"
	"fmt"
	"strings"

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
	MerchantID uint64               `json:"merchant_id"`
	Enabled    uint8                `json:"enabled"`
	Mode       string               `json:"mode"`
	Points     []model.GeoPoint     `json:"points"`
	Spots      []model.DeliverySpot `json:"spots"`
}

type DeliveryZoneCheckResult struct {
	InZone      bool `json:"in_zone"`
	ZoneEnabled bool `json:"zone_enabled"`
}

type UpsertDeliveryZoneInput struct {
	Enabled *uint8
	Mode    *string
	Points  []model.GeoPoint
	Spots   []model.DeliverySpot
	HasSpots bool // 区分「未传 spots」与「传空数组」
	HasPoints bool
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

// getByMerchantIDAny 含软删记录，用于 Upsert 复活唯一键冲突行。
func (s *DeliveryZoneService) getByMerchantIDAny(merchantID uint64) (*model.MerchantDeliveryZone, error) {
	var zone model.MerchantDeliveryZone
	if err := s.DB.Unscoped().Where("merchant_id = ?", merchantID).First(&zone).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryZoneNotFound
		}
		return nil, err
	}
	return &zone, nil
}

func (s *DeliveryZoneService) ToView(zone *model.MerchantDeliveryZone) DeliveryZoneView {
	if zone == nil {
		return DeliveryZoneView{Mode: model.DeliveryZoneModePolygon, Points: []model.GeoPoint{}, Spots: []model.DeliverySpot{}}
	}
	points := zone.Points
	if points == nil {
		points = []model.GeoPoint{}
	}
	spots := zone.Spots
	if spots == nil {
		spots = []model.DeliverySpot{}
	}
	mode := model.NormalizeDeliveryZoneMode(zone.Mode)
	return DeliveryZoneView{
		MerchantID: zone.MerchantID,
		Enabled:    zone.Enabled,
		Mode:       mode,
		Points:     points,
		Spots:      spots,
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

func normalizeSpots(spots []model.DeliverySpot) []model.DeliverySpot {
	out := make([]model.DeliverySpot, 0, len(spots))
	for _, sp := range spots {
		name := strings.TrimSpace(sp.Name)
		radius := sp.RadiusM
		if radius == 0 {
			radius = model.DeliverySpotDefaultM
		}
		out = append(out, model.DeliverySpot{
			Name: name, Latitude: sp.Latitude, Longitude: sp.Longitude, RadiusM: radius,
		})
	}
	return out
}

func validateZonePayload(enabled uint8, mode string, points []model.GeoPoint, spots []model.DeliverySpot) error {
	mode = model.NormalizeDeliveryZoneMode(mode)
	if mode == model.DeliveryZoneModeSpots {
		if enabled == model.DeliveryZoneEnabled {
			return geo.ValidateDeliverySpots(spots)
		}
		if len(spots) > 0 {
			return geo.ValidateDeliverySpots(spots)
		}
		return nil
	}
	if enabled == model.DeliveryZoneEnabled {
		return geo.ValidatePolygonPoints(points)
	}
	if len(points) > 0 {
		return geo.ValidatePolygonPoints(points)
	}
	return nil
}

func zoneIsActivelyRestricting(zone *model.MerchantDeliveryZone) bool {
	if zone == nil || zone.Enabled != model.DeliveryZoneEnabled {
		return false
	}
	mode := model.NormalizeDeliveryZoneMode(zone.Mode)
	if mode == model.DeliveryZoneModeSpots {
		return len(zone.Spots) >= 1
	}
	return len(zone.Points) >= 3
}

func (s *DeliveryZoneService) Upsert(merchantID uint64, input UpsertDeliveryZoneInput) (*DeliveryZoneView, error) {
	existing, err := s.getByMerchantIDAny(merchantID)
	if err != nil && !errors.Is(err, ErrDeliveryZoneNotFound) {
		return nil, err
	}
	active := existing != nil && existing.IsDeleted == model.NotDeleted

	enabled := model.DeliveryZoneEnabled
	if active {
		enabled = existing.Enabled
	}
	if input.Enabled != nil {
		if *input.Enabled != model.DeliveryZoneDisabled && *input.Enabled != model.DeliveryZoneEnabled {
			return nil, fmt.Errorf("%w: enabled 须为 0 或 1", ErrDeliveryZoneInvalid)
		}
		enabled = *input.Enabled
	}

	mode := model.DeliveryZoneModePolygon
	if active {
		mode = model.NormalizeDeliveryZoneMode(existing.Mode)
	} else if input.HasSpots && len(input.Spots) > 0 && !input.HasPoints {
		mode = model.DeliveryZoneModeSpots
	}
	if input.Mode != nil {
		mode = model.NormalizeDeliveryZoneMode(*input.Mode)
	}

	points := []model.GeoPoint{}
	if active && existing.Points != nil {
		points = existing.Points
	}
	if input.HasPoints {
		points = input.Points
		if points == nil {
			points = []model.GeoPoint{}
		}
	}

	spots := []model.DeliverySpot{}
	if active && existing.Spots != nil {
		spots = existing.Spots
	}
	if input.HasSpots {
		spots = normalizeSpots(input.Spots)
	}

	if err := validateZonePayload(enabled, mode, points, spots); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
	}

	if existing == nil {
		zone := model.MerchantDeliveryZone{
			MerchantID: merchantID,
			Enabled:    enabled,
			Mode:       mode,
			Points:     points,
			Spots:      spots,
		}
		if err := s.DB.Create(&zone).Error; err != nil {
			return nil, fmt.Errorf("保存配送范围失败: %w", err)
		}
		view := s.ToView(&zone)
		return &view, nil
	}

	existing.Enabled = enabled
	existing.Mode = mode
	existing.Points = points
	existing.Spots = spots
	existing.IsDeleted = model.NotDeleted
	if err := s.DB.Unscoped().Save(existing).Error; err != nil {
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
			if input.Mode == nil {
				m := model.DeliveryZoneModePolygon
				if input.HasSpots && len(input.Spots) > 0 {
					m = model.DeliveryZoneModeSpots
				}
				input.Mode = &m
			}
			if !input.HasPoints {
				input.Points = []model.GeoPoint{}
				input.HasPoints = true
			}
			if !input.HasSpots {
				input.Spots = []model.DeliverySpot{}
				input.HasSpots = true
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
	mode := model.NormalizeDeliveryZoneMode(existing.Mode)
	if input.Mode != nil {
		mode = model.NormalizeDeliveryZoneMode(*input.Mode)
	}
	points := existing.Points
	if input.HasPoints {
		points = input.Points
		if points == nil {
			points = []model.GeoPoint{}
		}
	}
	spots := existing.Spots
	if input.HasSpots {
		spots = normalizeSpots(input.Spots)
	}

	if err := validateZonePayload(enabled, mode, points, spots); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
	}

	existing.Enabled = enabled
	existing.Mode = mode
	existing.Points = points
	existing.Spots = spots
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
	if !zoneIsActivelyRestricting(zone) {
		return &DeliveryZoneCheckResult{InZone: true, ZoneEnabled: false}, nil
	}
	mode := model.NormalizeDeliveryZoneMode(zone.Mode)
	var in bool
	if mode == model.DeliveryZoneModeSpots {
		in = geo.PointInAnySpot(lat, lng, zone.Spots)
	} else {
		in = geo.PointInPolygon(lat, lng, zone.Points)
	}
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
	if !zoneIsActivelyRestricting(zone) {
		return nil
	}
	if lat == nil || lng == nil {
		return ErrDeliveryCoordinatesRequired
	}
	if err := geo.ValidateCoordinate(*lat, *lng); err != nil {
		return fmt.Errorf("%w: %s", ErrDeliveryZoneInvalid, err.Error())
	}
	mode := model.NormalizeDeliveryZoneMode(zone.Mode)
	ok := false
	if mode == model.DeliveryZoneModeSpots {
		ok = geo.PointInAnySpot(*lat, *lng, zone.Spots)
	} else {
		ok = geo.PointInPolygon(*lat, *lng, zone.Points)
	}
	if !ok {
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
