package geo

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestPointInPolygon(t *testing.T) {
	square := []model.GeoPoint{
		{Latitude: 0, Longitude: 0},
		{Latitude: 0, Longitude: 10},
		{Latitude: 10, Longitude: 10},
		{Latitude: 10, Longitude: 0},
	}
	if !PointInPolygon(5, 5, square) {
		t.Fatal("expected inside")
	}
	if PointInPolygon(15, 5, square) {
		t.Fatal("expected outside")
	}
	if !PointInPolygon(0, 5, square) {
		t.Fatal("expected on edge inside")
	}
}
