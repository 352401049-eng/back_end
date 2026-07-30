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

type TakeoutHandler struct {
	TakeoutSvc *service.TakeoutService
}

type CreateTakeoutRequest struct {
	MerchantID         uint64                          `json:"merchant_id"`
	ProductID          uint64                          `json:"product_id"`
	Quantity           uint32                          `json:"quantity"`
	AddressID          uint64                          `json:"address_id"`
	DeliveryTimeRemark string                          `json:"delivery_time_remark"`
	PackageSelections  []service.PackageSelectionInput `json:"package_selections"`
	OptionSelections   []service.OptionSelectionUnitInput `json:"option_selections"`
}

// CreateTakeout godoc
// @Summary      创建外卖订单
// @Tags         用户-外卖
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateTakeoutRequest  true  "下单信息"
// @Success      200   {object}  response.Body{data=service.TakeoutView}
// @Router       /user/takeout-orders [post]
func (h *TakeoutHandler) Create(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CreateTakeoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效: "+err.Error())
		return
	}
	view, err := h.TakeoutSvc.Create(accountID, service.CreateTakeoutInput{
		MerchantID:         req.MerchantID,
		ProductID:          req.ProductID,
		Quantity:           req.Quantity,
		AddressID:          req.AddressID,
		DeliveryTimeRemark: req.DeliveryTimeRemark,
		PackageSelections:  req.PackageSelections,
		OptionSelections:   req.OptionSelections,
	})
	if err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, view)
}

// PayTakeout godoc
// @Summary      外卖订单预支付
// @Tags         用户-外卖
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "外卖单 ID"
// @Success      200  {object}  response.Body
// @Router       /user/takeout-orders/{id}/pay [post]
func (h *TakeoutHandler) Pay(c *gin.Context) {
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
	result, err := h.TakeoutSvc.CreatePrepay(accountID, id)
	if err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, result)
}

// ListTakeouts godoc
// @Summary      外卖订单列表
// @Tags         用户-外卖
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /user/takeout-orders [get]
func (h *TakeoutHandler) List(c *gin.Context) {
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
	var status *uint8
	if s := c.Query("status"); s != "" {
		v, err := strconv.ParseUint(s, 10, 8)
		if err != nil {
			response.BadRequest(c, "status 参数无效")
			return
		}
		u := uint8(v)
		status = &u
	}
	pageNum, pageSize, _ := page.Normalize()
	list, total, err := h.TakeoutSvc.List(accountID, pageNum, pageSize, status)
	if err != nil {
		response.InternalError(c, "获取外卖订单失败")
		return
	}
	response.OK(c, &query.PageResult{List: list, Total: total, Page: pageNum, PageSize: pageSize})
}

// GetTakeout godoc
// @Summary      外卖订单详情
// @Tags         用户-外卖
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "外卖单 ID"
// @Success      200  {object}  response.Body{data=service.TakeoutView}
// @Router       /user/takeout-orders/{id} [get]
func (h *TakeoutHandler) Get(c *gin.Context) {
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
	view, err := h.TakeoutSvc.GetView(accountID, id)
	if err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, view)
}

func handleTakeoutError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTakeoutNotFound):
		response.Fail(c, 404, 404, "外卖订单不存在")
	case errors.Is(err, service.ErrTakeoutStatusInvalid):
		response.BadRequest(c, "当前状态不允许此操作")
	case errors.Is(err, service.ErrProductNotFound):
		response.Fail(c, 404, 404, "商品不存在")
	case errors.Is(err, service.ErrMerchantNotFound):
		response.Fail(c, 404, 404, "商家不存在")
	case errors.Is(err, service.ErrInsufficientStock):
		response.BadRequest(c, "库存不足")
	case errors.Is(err, service.ErrAddressRequired):
		response.BadRequest(c, "请选择收货地址")
	case errors.Is(err, service.ErrDeliveryOutOfRange):
		response.BadRequest(c, "收货地址不在配送范围内")
	case errors.Is(err, service.ErrDeliveryCoordinatesRequired):
		response.BadRequest(c, "配送订单请提供有效收货地址")
	case errors.Is(err, service.ErrPackageSelectionRequired), errors.Is(err, service.ErrOptionRequired), errors.Is(err, service.ErrOptionInvalid):
		msg := err.Error()
		response.BadRequest(c, msg)
	case errors.Is(err, service.ErrInvalidProductArg):
		msg := err.Error()
		if i := len(service.ErrInvalidProductArg.Error()); i+2 < len(msg) && msg[i:i+2] == ": " {
			msg = msg[i+2:]
		} else {
			msg = "参数无效"
		}
		response.BadRequest(c, msg)
	default:
		response.InternalError(c, "操作失败")
	}
}
