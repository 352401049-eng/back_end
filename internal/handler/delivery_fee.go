package handler

import (
	"errors"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type DeliveryFeeHandler struct {
	Svc *service.DeliveryFeePayService
}

type CreateDeliveryFeeRequest struct {
	MerchantID         uint64                      `json:"merchant_id"`
	AddressID          uint64                      `json:"address_id"`
	DeliveryTimeRemark string                      `json:"delivery_time_remark"`
	Items              []service.UseBatchItemInput   `json:"items" binding:"required"`
	Remark             *string                     `json:"remark"`
}

// CreateDeliveryFeeOrder godoc
// @Summary      创建跑腿配送费预支付单
// @Tags         用户-背包
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateDeliveryFeeRequest  true  "跑腿草稿"
// @Success      200   {object}  response.Body{data=service.DeliveryFeePayView}
// @Router       /user/delivery-fee-orders [post]
func (h *DeliveryFeeHandler) Create(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CreateDeliveryFeeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效: "+err.Error())
		return
	}
	view, err := h.Svc.Create(accountID, service.CreateDeliveryFeePayInput{
		MerchantID:         req.MerchantID,
		AddressID:          req.AddressID,
		DeliveryTimeRemark: req.DeliveryTimeRemark,
		Items:              req.Items,
		Remark:             req.Remark,
	})
	if err != nil {
		handleDeliveryFeeError(c, err)
		return
	}
	response.OK(c, view)
}

// PayDeliveryFeeOrder godoc
// @Summary      跑腿配送费预支付
// @Tags         用户-背包
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "配送费单 ID"
// @Success      200  {object}  response.Body
// @Router       /user/delivery-fee-orders/{id}/pay [post]
func (h *DeliveryFeeHandler) Pay(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	result, err := h.Svc.CreatePrepay(accountID, id)
	if err != nil {
		handleDeliveryFeeError(c, err)
		return
	}
	response.OK(c, result)
}

func handleDeliveryFeeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDeliveryFeeOrderNotFound):
		response.Fail(c, 404, 404, "配送费订单不存在")
	case errors.Is(err, service.ErrDeliveryFeeStatusInvalid):
		response.BadRequest(c, "当前状态不允许此操作")
	case errors.Is(err, service.ErrInventoryNotFound):
		response.Fail(c, 404, 404, "背包记录不存在")
	case errors.Is(err, service.ErrInventoryInsufficient):
		response.BadRequest(c, "背包数量不足")
	case errors.Is(err, service.ErrMerchantNotFound):
		response.Fail(c, 404, 404, "商家不存在")
	case errors.Is(err, service.ErrAddressRequired):
		response.BadRequest(c, "请选择收货地址")
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryFeePaymentRequired):
		response.BadRequest(c, "请先支付配送费")
	case errors.Is(err, service.ErrInventoryUsageInvalid):
		msg := err.Error()
		response.BadRequest(c, msg)
	default:
		handleInventoryError(c, err)
	}
}
