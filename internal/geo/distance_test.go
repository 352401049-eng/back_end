package geo

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestDistanceMeters_SamePoint(t *testing.T) {
	if d := DistanceMeters(23.1, 113.2, 23.1, 113.2); d > 0.01 {
		t.Fatalf("same point distance=%v", d)
	}
}

func TestDistanceMeters_ApproxKnown(t *testing.T) {
	// ~111.2km per degree latitude near equator; 0.001° ≈ 111m
	d := DistanceMeters(0, 0, 0.001, 0)
	if d < 100 || d > 120 {
		t.Fatalf("expected ~111m, got %v", d)
	}
}

func TestPointInAnySpot(t *testing.T) {
	spots := []model.DeliverySpot{
		{Name: "A", Latitude: 23.1, Longitude: 113.2, RadiusM: 300},
	}
	if !PointInAnySpot(23.1, 113.2, spots) {
		t.Fatal("center should be inside")
	}
	// ~1km away
	if PointInAnySpot(23.11, 113.2, spots) {
		t.Fatal("1km away should be outside 300m")
	}
	// ~50m (0.00045°)
	if !PointInAnySpot(23.10045, 113.2, spots) {
		t.Fatal("~50m should be inside 300m")
	}
}

func TestValidateDeliverySpots(t *testing.T) {
	if err := ValidateDeliverySpots(nil); err == nil {
		t.Fatal("empty should fail")
	}
	ok := []model.DeliverySpot{{Name: "x", Latitude: 23, Longitude: 113, RadiusM: 300}}
	if err := ValidateDeliverySpots(ok); err != nil {
		t.Fatal(err)
	}
	bad := []model.DeliverySpot{{Name: "x", Latitude: 23, Longitude: 113, RadiusM: 50}}
	if err := ValidateDeliverySpots(bad); err == nil {
		t.Fatal("radius 50 should fail")
	}
}
