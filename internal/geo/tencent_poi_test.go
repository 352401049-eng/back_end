package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestSearchPOI_SpotsMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("key") == "" {
			t.Errorf("missing key param")
		}
		// 验证 boundary 是 nearby(lat,lng,radius) 格式
		b := q.Get("boundary")
		if len(b) < 8 || b[:6] != "nearby" {
			t.Errorf("boundary not nearby format: %s", b)
		}
		resp := map[string]interface{}{
			"status": 0,
			"data": []map[string]interface{}{
				{"title": "阳光小区", "category": "住宅小区:住宅小区", "location": map[string]float64{"lat": 23.13, "lng": 113.26}},
				{"title": "中心大厦", "category": "商务大厦:商务楼宇", "location": map[string]float64{"lat": 23.131, "lng": 113.261}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	spots := []model.DeliverySpot{
		{Name: "点1", Latitude: 23.13, Longitude: 113.26, RadiusM: 500},
	}
	landmarks, err := SearchPOI(context.Background(), srv.Client(), srv.URL, "test-key", POIRequest{
		Mode:  model.DeliveryZoneModeSpots,
		Spots: spots,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(landmarks) != 2 {
		t.Fatalf("want 2 landmarks, got %d", len(landmarks))
	}
	if landmarks[0].Name != "阳光小区" {
		t.Errorf("first landmark name = %s, want 阳光小区", landmarks[0].Name)
	}
	if landmarks[0].Latitude != 23.13 {
		t.Errorf("first landmark lat = %v, want 23.13", landmarks[0].Latitude)
	}
}

func TestSearchPOI_PolygonMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": 0,
			"data":   []map[string]interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	points := []model.GeoPoint{
		{Latitude: 23.13, Longitude: 113.26},
		{Latitude: 23.14, Longitude: 113.26},
		{Latitude: 23.135, Longitude: 113.27},
	}
	landmarks, err := SearchPOI(context.Background(), srv.Client(), srv.URL, "test-key", POIRequest{
		Mode:   model.DeliveryZoneModePolygon,
		Points: points,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if landmarks == nil {
		t.Errorf("want empty slice, got nil")
	}
}

func TestSearchPOI_EmptyKeyReturnsError(t *testing.T) {
	_, err := SearchPOI(context.Background(), http.DefaultClient, "http://example.com", "", POIRequest{
		Mode: model.DeliveryZoneModeSpots,
		Spots: []model.DeliverySpot{
			{Name: "点1", Latitude: 23.13, Longitude: 113.26, RadiusM: 500},
		},
	})
	if err != ErrMapKeyNotConfigured {
		t.Fatalf("want ErrMapKeyNotConfigured, got %v", err)
	}
}

func TestSearchPOI_EmptySpotsAndPointsReturnsError(t *testing.T) {
	_, err := SearchPOI(context.Background(), http.DefaultClient, "http://example.com", "key", POIRequest{
		Mode: model.DeliveryZoneModeSpots,
	})
	if err == nil {
		t.Fatalf("want error for empty spots/points")
	}
}

func TestSearchPOI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := SearchPOI(context.Background(), srv.Client(), srv.URL, "key", POIRequest{
		Mode: model.DeliveryZoneModeSpots,
		Spots: []model.DeliverySpot{
			{Name: "点1", Latitude: 23.13, Longitude: 113.26, RadiusM: 500},
		},
	})
	if err == nil {
		t.Fatalf("want error on http 500")
	}
}

func TestSearchPOI_TencentStatusNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"status": 120, "message": "daily quota exceeded"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	_, err := SearchPOI(context.Background(), srv.Client(), srv.URL, "key", POIRequest{
		Mode: model.DeliveryZoneModeSpots,
		Spots: []model.DeliverySpot{
			{Name: "点1", Latitude: 23.13, Longitude: 113.26, RadiusM: 500},
		},
	})
	if err == nil {
		t.Fatalf("want error on tencent status non-zero")
	}
}

func TestComputeSearchCenter_SpotsMode(t *testing.T) {
	spots := []model.DeliverySpot{
		{Name: "点1", Latitude: 23.13, Longitude: 113.26, RadiusM: 300},
		{Name: "点2", Latitude: 23.14, Longitude: 113.27, RadiusM: 500},
	}
	lat, lng, radius := computeSearchCenter(POIRequest{
		Mode:  model.DeliveryZoneModeSpots,
		Spots: spots,
	})
	if lat != 23.13 || lng != 113.26 {
		t.Errorf("center = (%v, %v), want (23.13, 113.26) - first spot", lat, lng)
	}
	if radius != 500 {
		t.Errorf("radius = %v, want 500 - max spot radius", radius)
	}
}

func TestComputeSearchCenter_PolygonMode(t *testing.T) {
	points := []model.GeoPoint{
		{Latitude: 23.13, Longitude: 113.26},
		{Latitude: 23.14, Longitude: 113.26},
		{Latitude: 23.135, Longitude: 113.28},
	}
	lat, lng, radius := computeSearchCenter(POIRequest{
		Mode:   model.DeliveryZoneModePolygon,
		Points: points,
	})
	// 质心 = 平均
	wantLat := (23.13 + 23.14 + 23.135) / 3
	if lat < wantLat-0.001 || lat > wantLat+0.001 {
		t.Errorf("centroid lat = %v, want ~%v", lat, wantLat)
	}
	_ = lng
	// 半径上限 1000
	if radius > 1000 {
		t.Errorf("radius = %v, should be capped at 1000", radius)
	}
}
