package handler

import (
	"errors"
	"strconv"
	"strings"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type RefundInventoryRequest struct {
	Quantity uint32                             `json:"quantity" example:"1"`
	OrderID  *uint64                            `json:"order_id" example:"12"` // 单来源；不传则 FIFO
	Items    []service.InventoryRefundItemInput `json:"items"`                 // 多来源精确退，优先于 quantity/order_id
}

// ListInventoryRefundSources godoc
// @Summary      背包退款来源批次
// @Description  同一商品不同成交价/渠道分批列出，供用户多选退款
// @Tags         用户-背包
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "背包记录 ID"
// @Success      200  {object}  response.Body{data=service.InventoryRefundSourcesView}
// @Router       /user/inventory/{id}/refund-sources [get]
func (h *UserHandler) ListInventoryRefundSources(c *gin.Context) {
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
	if h.OrderSvc == nil {
		response.InternalError(c, "服务未配置")
		return
	}
	view, err := h.OrderSvc.ListInventoryRefundSources(accountID, inventoryID)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// RefundInventory godoc
// @Summary      背包未使用商品退款
// @Description  支持 items 多来源精确退，或 order_id/quantity 单来源，或 FIFO
// @Tags         用户-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                       true  "背包记录 ID"
// @Param        body  body  RefundInventoryRequest    false "退款数量与来源"
// @Success      200   {object}  response.Body{data=service.InventoryRefundView}
// @Router       /user/inventory/{id}/refund [post]
func (h *UserHandler) RefundInventory(c *gin.Context) {
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
	if h.OrderSvc == nil || h.InventorySvc == nil {
		response.InternalError(c, "服务未配置")
		return
	}
	var req RefundInventoryRequest
	_ = c.ShouldBindJSON(&req)

	items := make([]service.InventoryRefundItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		if it.OrderID == 0 || it.Quantity == 0 {
			continue
		}
		items = append(items, it)
	}

	qty := req.Quantity
	if len(items) == 0 && qty == 0 {
		inv, err := h.InventorySvc.GetOwned(accountID, inventoryID)
		if err != nil {
			handleInventoryError(c, err)
			return
		}
		qty = inv.Quantity
		if qty == 0 {
			response.BadRequest(c, "无可退数量")
			return
		}
	}
	view, err := h.OrderSvc.RefundInventory(accountID, inventoryID, qty, req.OrderID, items)
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

type CancelInventoryUsageRequest struct {
	Reason *string `json:"reason" example:"临时有事"`
}

type UseInventoryRequest struct {
	Quantity uint32 `json:"quantity" example:"1"`
	// 用指针 + required：允许传 0（稍后/到店核销），避免 uint8 零值被 required 误拒
	DeliveryType      *uint8                         `json:"delivery_type" binding:"required" example:"1"`
	AddressID         *uint64                        `json:"address_id"`
	DeliveryLatitude  *float64                       `json:"delivery_latitude"`
	DeliveryLongitude *float64                       `json:"delivery_longitude"`
	Remark            *string                        `json:"remark"`
	PackageSelections []service.PackageSelectionInput `json:"package_selections"`
	OptionSelections  []service.OptionSelectionUnitInput `json:"option_selections"`
}

type UseBatchInventoryRequest struct {
	Items             []service.UseBatchItemInput `json:"items" binding:"required"`
	DeliveryType      *uint8                      `json:"delivery_type" binding:"required"`
	AddressID         *uint64                     `json:"address_id"`
	DeliveryLatitude  *float64                    `json:"delivery_latitude"`
	DeliveryLongitude *float64                    `json:"delivery_longitude"`
	Remark            *string                     `json:"remark"`
}

type ConfirmPackageSelectionRequest struct {
	PackageSelections []service.PackageSelectionInput `json:"package_selections"`
	PackageUnits      []service.PackageUnitInput      `json:"package_units"`
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
		Quantity: req.Quantity, DeliveryType: *req.DeliveryType,
		AddressID: req.AddressID, DeliveryLatitude: req.DeliveryLatitude, DeliveryLongitude: req.DeliveryLongitude,
		Remark: req.Remark, PackageSelections: req.PackageSelections, OptionSelections: req.OptionSelections,
	})
	if err != nil {
		handleInventoryError(c, err)
		return
	}
	response.OK(c, view)
}

// UseInventoryBatch godoc
// @Summary      批量使用背包商品（同店）
// @Tags         用户-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  UseBatchInventoryRequest  true  "批量使用"
// @Success      200   {object}  response.Body{data=service.UseBatchResult}
// @Router       /user/inventory/use-batch [post]
func (h *UserHandler) UseInventoryBatch(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req UseBatchInventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	view, err := h.InventorySvc.UseBatch(accountID, service.UseBatchInput{
		Items: req.Items, DeliveryType: *req.DeliveryType,
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
	case errors.Is(err, service.ErrInventoryRefundInvalid):
		msg := err.Error()
		if i := strings.Index(msg, ": "); i >= 0 && i+2 < len(msg) {
			response.BadRequest(c, msg[i+2:])
			return
		}
		response.BadRequest(c, "当前不可退款")
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
	case errors.Is(err, service.ErrDeliveryNotAllowed):
		response.BadRequest(c, "该商品不支持骑手配送")
	case errors.Is(err, service.ErrPickupNotAllowed):
		response.BadRequest(c, "该商品不支持到店自取")
	case errors.Is(err, service.ErrPackageSelectionRequired):
		response.BadRequest(c, "套餐外卖请先完成选配")
	case errors.Is(err, service.ErrOptionRequired):
		response.BadRequest(c, "请先完成规格选择")
	case errors.Is(err, service.ErrOptionInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrInsufficientStock):
		response.BadRequest(c, "套餐内商品库存不足")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryCoordinatesRequired):
		response.BadRequest(c, "配送地址缺少坐标，请在地图上选点保存收货地址")
	case errors.Is(err, service.ErrDeliveryZoneInvalid):
		response.BadRequest(c, err.Error())
	default:
		msg := err.Error()
		if msg != "" && (errors.Is(err, service.ErrInventoryUsageInvalid) || len(msg) < 80) {
			response.BadRequest(c, msg)
			return
		}
		response.InternalError(c, "操作失败")
	}
}
