package handler

import (
	"errors"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type DeliveryZoneHandler struct {
	ZoneSvc     *service.DeliveryZoneService
	MerchantSvc *service.MerchantService
}

type deliveryZonePointBody struct {
	Latitude  FlexFloat64 `json:"latitude"`
	Longitude FlexFloat64 `json:"longitude"`
	Lat       FlexFloat64 `json:"lat"`
	Lng       FlexFloat64 `json:"lng"`
}

type upsertDeliveryZoneBody struct {
	Enabled FlexUInt8Ptr            `json:"enabled"`
	Points  []deliveryZonePointBody `json:"points"`
}

type checkDeliveryZoneBody struct {
	Latitude  FlexFloat64 `json:"latitude"`
	Longitude FlexFloat64 `json:"longitude"`
	Lat       FlexFloat64 `json:"lat"`
	Lng       FlexFloat64 `json:"lng"`
}

func parseGeoPointBody(p deliveryZonePointBody) model.GeoPoint {
	lat := p.Latitude.Float64()
	if lat == 0 && p.Lat.Float64() != 0 {
		lat = p.Lat.Float64()
	}
	lng := p.Longitude.Float64()
	if lng == 0 && p.Lng.Float64() != 0 {
		lng = p.Lng.Float64()
	}
	return model.GeoPoint{Latitude: lat, Longitude: lng}
}

func parseCheckCoordinates(body checkDeliveryZoneBody) (float64, float64) {
	lat := body.Latitude.Float64()
	if lat == 0 && body.Lat.Float64() != 0 {
		lat = body.Lat.Float64()
	}
	lng := body.Longitude.Float64()
	if lng == 0 && body.Lng.Float64() != 0 {
		lng = body.Lng.Float64()
	}
	return lat, lng
}

func parseUpsertDeliveryZoneBody(c *gin.Context) (service.UpsertDeliveryZoneInput, error) {
	var raw upsertDeliveryZoneBody
	if err := c.ShouldBindJSON(&raw); err != nil {
		return service.UpsertDeliveryZoneInput{}, err
	}
	input := service.UpsertDeliveryZoneInput{}
	if raw.Enabled.Set {
		v := raw.Enabled.Value
		input.Enabled = &v
	}
	if raw.Points != nil {
		points := make([]model.GeoPoint, 0, len(raw.Points))
		for _, p := range raw.Points {
			points = append(points, parseGeoPointBody(p))
		}
		input.Points = points
	}
	return input, nil
}

// GetMerchantDeliveryZone godoc
// @Summary      获取本店配送范围
// @Tags         商家端-配送范围
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /merchant/delivery-zone [get]
func (h *DeliveryZoneHandler) GetMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	view, err := h.ZoneSvc.GetView(*scope)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// PutMerchantDeliveryZone godoc
// @Summary      保存配送范围（全量）
// @Description  创建或覆盖多边形顶点；enabled=1 时 points 至少 3 点
// @Tags         商家端-配送范围
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  upsertDeliveryZoneBody  true  "配送范围"
// @Success      200   {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /merchant/delivery-zone [put]
func (h *DeliveryZoneHandler) PutMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	input, err := parseUpsertDeliveryZoneBody(c)
	if err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if input.Points == nil {
		response.BadRequest(c, "请传 points 顶点数组")
		return
	}
	view, err := h.ZoneSvc.Upsert(*scope, input)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// PatchMerchantDeliveryZone godoc
// @Summary      部分更新配送范围
// @Tags         商家端-配送范围
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  upsertDeliveryZoneBody  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /merchant/delivery-zone [patch]
func (h *DeliveryZoneHandler) PatchMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	input, err := parseUpsertDeliveryZoneBody(c)
	if err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if input.Enabled == nil && input.Points == nil {
		response.BadRequest(c, "请至少传 enabled 或 points")
		return
	}
	view, err := h.ZoneSvc.Patch(*scope, input)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// DeleteMerchantDeliveryZone godoc
// @Summary      删除配送范围
// @Tags         商家端-配送范围
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body
// @Router       /merchant/delivery-zone [delete]
func (h *DeliveryZoneHandler) DeleteMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	if err := h.ZoneSvc.Delete(*scope); err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, nil)
}

// GetAdminDeliveryZone godoc
// @Summary      获取商家配送范围
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /admin/merchants/{id}/delivery-zone [get]
func (h *DeliveryZoneHandler) GetAdmin(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.ZoneSvc.GetView(merchantID)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// PutAdminDeliveryZone godoc
// @Summary      保存商家配送范围
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                     true  "商家 ID"
// @Param        body  body  upsertDeliveryZoneBody  true  "配送范围"
// @Success      200   {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /admin/merchants/{id}/delivery-zone [put]
func (h *DeliveryZoneHandler) PutAdmin(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	input, err := parseUpsertDeliveryZoneBody(c)
	if err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if input.Points == nil {
		response.BadRequest(c, "请传 points 顶点数组")
		return
	}
	view, err := h.ZoneSvc.Upsert(merchantID, input)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// PatchAdminDeliveryZone godoc
// @Summary      部分更新商家配送范围
// @Tags         管理端-商家
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                     true  "商家 ID"
// @Param        body  body  upsertDeliveryZoneBody  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /admin/merchants/{id}/delivery-zone [patch]
func (h *DeliveryZoneHandler) PatchAdmin(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	input, err := parseUpsertDeliveryZoneBody(c)
	if err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if input.Enabled == nil && input.Points == nil {
		response.BadRequest(c, "请至少传 enabled 或 points")
		return
	}
	view, err := h.ZoneSvc.Patch(merchantID, input)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// DeleteAdminDeliveryZone godoc
// @Summary      删除商家配送范围
// @Tags         管理端-商家
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/merchants/{id}/delivery-zone [delete]
func (h *DeliveryZoneHandler) DeleteAdmin(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.ZoneSvc.Delete(merchantID); err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, nil)
}

// GetPublicDeliveryZone godoc
// @Summary      商家配送范围（用户端）
// @Description  仅返回已启用的配送多边形，供店铺页地图展示
// @Tags         用户-商城
// @Produce      json
// @Param        id  path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryZoneView}
// @Router       /merchants/{id}/delivery-zone [get]
func (h *DeliveryZoneHandler) GetPublic(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	if _, err := h.MerchantSvc.GetOpenByID(merchantID); err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	view, err := h.ZoneSvc.GetPublicView(merchantID)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, view)
}

// CheckPublicDeliveryZone godoc
// @Summary      校验坐标是否在配送范围内
// @Tags         用户-商城
// @Accept       json
// @Produce      json
// @Param        id    path  int                   true  "商家 ID"
// @Param        body  body  checkDeliveryZoneBody true  "坐标"
// @Success      200   {object}  response.Body{data=service.DeliveryZoneCheckResult}
// @Router       /merchants/{id}/delivery-zone/check [post]
func (h *DeliveryZoneHandler) CheckPublic(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	if _, err := h.MerchantSvc.GetOpenByID(merchantID); err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	var body checkDeliveryZoneBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	lat, lng := parseCheckCoordinates(body)
	result, err := h.ZoneSvc.CheckPoint(merchantID, lat, lng)
	if err != nil {
		handleDeliveryZoneError(c, err)
		return
	}
	response.OK(c, result)
}

func handleDeliveryZoneError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDeliveryZoneNotFound):
		response.Fail(c, 404, 404, "尚未设置配送范围")
	case errors.Is(err, service.ErrDeliveryZoneInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryCoordinatesRequired):
		response.BadRequest(c, "请选择配送地址坐标")
	default:
		response.InternalError(c, "操作失败")
	}
}
