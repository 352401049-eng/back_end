package handler

import (
	"errors"
	"strconv"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type CancelInventoryUsageRequest struct {
	Reason *string `json:"reason" example:"临时有事"`
}

type UseInventoryRequest struct {
	Quantity          uint32   `json:"quantity" example:"1"`
	DeliveryType      uint8    `json:"delivery_type" binding:"required" example:"1"`
	AddressID         *uint64  `json:"address_id"`
	DeliveryLatitude  *float64 `json:"delivery_latitude"`
	DeliveryLongitude *float64 `json:"delivery_longitude"`
	Remark            *string  `json:"remark"`
}

// UseInventory godoc
// @Summary      使用背包商品
// @Description  指定数量扣减库存并创建使用记录；自提时返回核销码
// @Tags         用户-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "背包记录 ID"
// @Param        body  body  UseInventoryRequest  true  "使用方式"
// @Success      200   {object}  response.Body{data=service.InventoryUsageView}
// @Router       /user/inventory/{id}/use [post]
func (h *UserHandler) UseInventory(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	inventoryID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "背包 ID 无效")
		return
	}
	var req UseInventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.InventorySvc.Use(accountID, inventoryID, service.UseInventoryInput{
		Quantity: req.Quantity, DeliveryType: req.DeliveryType,
		AddressID: req.AddressID, DeliveryLatitude: req.DeliveryLatitude, DeliveryLongitude: req.DeliveryLongitude,
		Remark: req.Remark,
	})
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// ListInventoryUsages godoc
// @Summary      背包使用记录列表
// @Tags         用户-背包
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /user/inventory/usages [get]
func (h *UserHandler) ListInventoryUsages(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var page query.Page
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BadRequest(c, "分页参数无效")
		return
	}
	p, pageSize, _ := page.Normalize()
	list, total, err := h.InventorySvc.ListUsages(accountID, p, pageSize)
	if err != nil {
		response.InternalError(c, "获取使用记录失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: p, PageSize: pageSize})
}

// GetInventoryUsage godoc
// @Summary      背包使用记录详情
// @Tags         用户-背包
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "使用记录 ID"
// @Success      200  {object}  response.Body{data=service.InventoryUsageView}
// @Router       /user/inventory/usages/{id} [get]
func (h *UserHandler) GetInventoryUsage(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	usageID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "使用记录 ID 无效")
		return
	}
	view, err := h.InventorySvc.GetUsageView(accountID, usageID)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// CancelInventoryUsage godoc
// @Summary      取消背包使用
// @Description  自提/未接单配送：立即取消并回滚库存；骑手已接单至用户确认收货前：提交取消申请，商家审核通过后回滚库存
// @Tags         用户-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                           true  "使用记录 ID"
// @Param        body  body  CancelInventoryUsageRequest   false  "取消原因"
// @Success      200   {object}  response.Body{data=service.InventoryUsageView}
// @Router       /user/inventory/usages/{id}/cancel [post]
func (h *UserHandler) CancelInventoryUsage(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	usageID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "使用记录 ID 无效")
		return
	}
	var req CancelInventoryUsageRequest
	_ = c.ShouldBindJSON(&req)
	view, err := h.InventorySvc.RequestCancelUsage(accountID, usageID, req.Reason)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

func handleInventoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInventoryNotFound):
		response.Fail(c, 404, 404, "背包记录不存在")
	case errors.Is(err, service.ErrInventoryInsufficient):
		response.BadRequest(c, "背包数量不足")
	case errors.Is(err, service.ErrInventoryUsageNotFound):
		response.Fail(c, 404, 404, "使用记录不存在")
	case errors.Is(err, service.ErrInventoryUsageInvalid):
		response.BadRequest(c, "当前状态不可取消")
	case errors.Is(err, service.ErrInventoryCancelPending):
		response.BadRequest(c, "取消申请审核中，请耐心等待")
	case errors.Is(err, service.ErrAddressRequired):
		response.BadRequest(c, "请选择收货地址")
	case errors.Is(err, service.ErrInvalidDeliveryType):
		response.BadRequest(c, "delivery_type 无效，请传 1=自提 或 2=配送")
	case errors.Is(err, service.ErrVirtualNotDeliverable):
		response.BadRequest(c, "该商品为虚拟商品，仅支持到店核销")
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryCoordinatesRequired):
		response.BadRequest(c, "配送地址缺少坐标，请在地图上选点保存收货地址")
	case errors.Is(err, service.ErrDeliveryZoneInvalid):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "操作失败")
	}
}
