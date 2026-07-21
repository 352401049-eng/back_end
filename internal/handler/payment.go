package handler

import (
	"errors"
	"io"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PaymentHandler struct {
	OrderSvc *service.OrderService
}

// PaymentProvider godoc
// @Summary      当前支付渠道
// @Description  provider=mock|wechat；immediate_settle=true 表示下单事务内已记已付（模拟支付）
// @Tags         用户-支付
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body
// @Router       /user/payment/provider [get]
func (h *PaymentHandler) Provider(c *gin.Context) {
	if _, ok := auth.AccountID(c); !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	response.OK(c, h.OrderSvc.PaymentProviderInfo())
}

// CreatePrepay godoc
// @Summary      发起预支付
// @Description  Mock：幂等补结算并返回 already_paid；微信：预留 wx.requestPayment 参数（未配置时 503）
// @Tags         用户-支付
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=payment.PrepayResult}
// @Router       /user/orders/{id}/pay [post]
func (h *PaymentHandler) CreatePrepay(c *gin.Context) {
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
	result, err := h.OrderSvc.CreatePrepay(accountID, id)
	if err != nil {
		handlePaymentError(c, err)
		return
	}
	response.OK(c, result)
}

// WeChatNotify godoc
// @Summary      微信支付回调（预留）
// @Description  验签与入账未实现；启用微信支付前请勿依赖
// @Tags         支付回调
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.Body
// @Router       /payments/wechat/notify [post]
func (h *PaymentHandler) WeChatNotify(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.BadRequest(c, "读取回调失败")
		return
	}
	headers := map[string]string{}
	for k, vs := range c.Request.Header {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	result, err := h.OrderSvc.HandlePaymentNotify(headers, body)
	if err != nil {
		handlePaymentError(c, err)
		return
	}
	response.OK(c, result)
}

func handlePaymentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Fail(c, 404, 404, "订单不存在")
	case errors.Is(err, payment.ErrNotConfigured):
		response.Fail(c, 503, 503, "微信支付未配置")
	case errors.Is(err, payment.ErrNotSupported):
		response.BadRequest(c, "当前支付渠道不支持此操作")
	case errors.Is(err, payment.ErrInvalidState):
		response.BadRequest(c, "订单支付状态无效")
	default:
		response.InternalError(c, "支付操作失败")
	}
}
