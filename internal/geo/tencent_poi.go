package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"yujixinjiang/backend/internal/model"
)

// ErrMapKeyNotConfigured 腾讯地图 Key 未配置。
var ErrMapKeyNotConfigured = errors.New("tencent map key not configured")

// ErrInvalidPOIRequest POI 检索请求参数无效。
var ErrInvalidPOIRequest = errors.New("invalid poi request")

// 腾讯 POI 接口半径上限 1000 米。
const tencentPOIRadiusMaxM = 1000

// 腾讯 POI 接口默认检索的分类（住宅小区、商务大厦、学校、商场、医院）。
const tencentPOICategory = "住宅小区:住宅小区,商务大厦:商务楼宇,学校:学校,购物中心:购物中心,医疗:医院"

// POIRequest POI 检索请求。
type POIRequest struct {
	Mode   string               // polygon | spots
	Points []model.GeoPoint     // polygon 顶点
	Spots  []model.DeliverySpot // 配送点
}

type tencentPOIResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    []struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Location struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"location"`
	} `json:"data"`
}

// computeSearchCenter 根据模式计算检索中心与半径（米）。
// spots 模式：第一个配送点为圆心，取最大半径（上限 1000）。
// polygon 模式：质心为圆心，外接矩形对角线一半为半径（上限 1000）。
func computeSearchCenter(req POIRequest) (lat, lng float64, radiusM int) {
	if req.Mode == model.DeliveryZoneModeSpots && len(req.Spots) > 0 {
		first := req.Spots[0]
		lat = first.Latitude
		lng = first.Longitude
		radiusM = 0
		for _, s := range req.Spots {
			if int(s.RadiusM) > radiusM {
				radiusM = int(s.RadiusM)
			}
		}
		if radiusM > tencentPOIRadiusMaxM {
			radiusM = tencentPOIRadiusMaxM
		}
		return
	}
	if len(req.Points) > 0 {
		var sumLat, sumLng float64
		minLat, maxLat := req.Points[0].Latitude, req.Points[0].Latitude
		minLng, maxLng := req.Points[0].Longitude, req.Points[0].Longitude
		for _, p := range req.Points {
			sumLat += p.Latitude
			sumLng += p.Longitude
			if p.Latitude < minLat {
				minLat = p.Latitude
			}
			if p.Latitude > maxLat {
				maxLat = p.Latitude
			}
			if p.Longitude < minLng {
				minLng = p.Longitude
			}
			if p.Longitude > maxLng {
				maxLng = p.Longitude
			}
		}
		lat = sumLat / float64(len(req.Points))
		lng = sumLng / float64(len(req.Points))
		// 外接矩形对角线一半，转米
		diagM := DistanceMeters(minLat, minLng, maxLat, maxLng)
		radiusM = int(math.Ceil(diagM / 2))
		if radiusM > tencentPOIRadiusMaxM {
			radiusM = tencentPOIRadiusMaxM
		}
		return
	}
	return 0, 0, 0
}

// SearchPOI 调腾讯位置服务 POI 检索接口。
// endpoint 可被测试用 httptest 替换；生产固定 https://apis.map.qq.com/place/v1/explore。
func SearchPOI(ctx context.Context, client *http.Client, endpoint, key string, req POIRequest) ([]model.Landmark, error) {
	if key == "" {
		return nil, ErrMapKeyNotConfigured
	}
	lat, lng, radiusM := computeSearchCenter(req)
	if lat == 0 && lng == 0 {
		return nil, fmt.Errorf("%w: spots 或 points 为空", ErrInvalidPOIRequest)
	}

	boundary := fmt.Sprintf("nearby(%.6f,%.6f,%d)", lat, lng, radiusM)
	params := url.Values{
		"key":       {key},
		"boundary":  {boundary},
		"category":  {tencentPOICategory},
		"page_size": {"20"},
	}
	requestURL := endpoint + "?" + params.Encode()

	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build poi request: %w", err)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call tencent poi: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read poi response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencent poi http %d: %s", resp.StatusCode, string(body))
	}
	var parsed tencentPOIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse poi response: %w", err)
	}
	if parsed.Status != 0 {
		return nil, fmt.Errorf("tencent poi status %d: %s", parsed.Status, parsed.Message)
	}
	landmarks := make([]model.Landmark, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		landmarks = append(landmarks, model.Landmark{
			Name:      d.Title,
			Latitude:  d.Location.Lat,
			Longitude: d.Location.Lng,
			Category:  d.Category,
		})
	}
	return landmarks, nil
}
