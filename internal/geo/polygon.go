package geo

import (
	"fmt"
	"math"

	"yujixinjiang/backend/internal/model"
)

// PointInPolygon 射线法判断点是否在多边形内（含边界）。
func PointInPolygon(lat, lng float64, polygon []model.GeoPoint) bool {
	n := len(polygon)
	if n < 3 {
		return false
	}
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		latI, lngI := polygon[i].Latitude, polygon[i].Longitude
		latJ, lngJ := polygon[j].Latitude, polygon[j].Longitude
		if pointOnSegment(lat, lng, latI, lngI, latJ, lngJ) {
			return true
		}
		if ((lngI > lng) != (lngJ > lng)) &&
			lat < (latJ-latI)*(lng-lngI)/(lngJ-lngI)+latI {
			inside = !inside
		}
		j = i
	}
	return inside
}

func pointOnSegment(lat, lng, lat1, lng1, lat2, lng2 float64) bool {
	const eps = 1e-9
	cross := (lng-lng1)*(lat2-lat1) - (lat-lat1)*(lng2-lng1)
	if math.Abs(cross) > eps {
		return false
	}
	dot := (lng-lng1)*(lng2-lng1) + (lat-lat1)*(lat2-lat1)
	if dot < -eps {
		return false
	}
	lenSq := (lng2-lng1)*(lng2-lng1) + (lat2-lat1)*(lat2-lat1)
	return dot-lenSq <= eps
}

// ValidatePolygonPoints 校验配送多边形顶点。
func ValidatePolygonPoints(points []model.GeoPoint) error {
	if len(points) < 3 {
		return fmt.Errorf("配送范围至少需要 3 个顶点")
	}
	for i, p := range points {
		if err := ValidateCoordinate(p.Latitude, p.Longitude); err != nil {
			return fmt.Errorf("第 %d 个顶点: %w", i+1, err)
		}
	}
	return nil
}

// ValidateCoordinate 校验单点经纬度范围。
func ValidateCoordinate(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return fmt.Errorf("latitude 须在 -90~90 之间")
	}
	if lng < -180 || lng > 180 {
		return fmt.Errorf("longitude 须在 -180~180 之间")
	}
	return nil
}
