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

type CompleteDeliveryRequest struct {
	Remark *string  `json:"remark" example:"已放门口"`
	Photos []string `json:"photos" example:"/uploads/2026/07/01/proof.jpg"`
}

// ListUserDeliveries godoc
// @Summary      我的配送单列表
// @Description  scope=active|delivering 配送中；pending_confirm 待确认收货；history 已完成
// @Tags         用户-配送
// @Produce      json
// @Security     BearerAuth
// @Param        scope      query  string  false  "active|pending_confirm|history"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /user/deliveries [get]
func (h *UserHandler) ListUserDeliveries(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.DeliverySvc.ListForUser(accountID, c.Query("scope"), page, pageSize)
	if err != nil {
		response.InternalError(c, "获取配送单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetUserDelivery godoc
// @Summary      配送单详情
// @Tags         用户-配送
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /user/deliveries/{id} [get]
func (h *UserHandler) GetUserDelivery(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.DeliverySvc.GetForUser(accountID, id)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, view)
}

// ConfirmDeliveryReceipt godoc
// @Summary      确认收货
// @Description  骑手送达后用户确认；配送单、关联订单/使用记录一并完成
// @Tags         用户-配送
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /user/deliveries/{id}/confirm [post]
func (h *UserHandler) ConfirmDeliveryReceipt(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.DeliverySvc.ConfirmReceipt(accountID, id)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, view)
}

// ConfirmOrderReceipt godoc
// @Summary      按订单确认收货
// @Description  购买订单配送场景，等价于确认关联配送单
// @Tags         用户-订单
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=service.DeliveryView}
// @Router       /user/orders/{id}/confirm-receipt [post]
func (h *UserHandler) ConfirmOrderReceipt(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	orderID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "订单 ID 无效")
		return
	}
	view, err := h.DeliverySvc.ConfirmReceiptByOrderID(accountID, orderID)
	if err != nil {
		handleDeliveryError(c, err)
		return
	}
	response.OK(c, view)
}

func handleDeliveryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDeliveryNotFound):
		response.Fail(c, 404, 404, "配送单不存在")
	case errors.Is(err, service.ErrDeliveryForbidden):
		response.Fail(c, 403, 403, "无权操作该配送单")
	case errors.Is(err, service.ErrDeliveryStatusInvalid):
		response.BadRequest(c, "当前状态不允许此操作")
	case errors.Is(err, service.ErrDeliveryTaken):
		response.BadRequest(c, "配送单已被接单")
	default:
		response.InternalError(c, "操作失败")
	}
}
