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
	TakeoutSvc  *service.TakeoutService
	MerchantSvc *service.MerchantService
}

type CreateTakeoutRequest struct {
	MerchantID         uint64                          `json:"merchant_id"`
	ProductID          uint64                          `json:"product_id"`
	Quantity           uint32                          `json:"quantity"`
	AddressID          uint64                          `json:"address_id"`
	DeliveryTimeRemark string                          `json:"delivery_time_remark"`
	PackageSelections  []service.PackageSelectionInput `json:"package_selections"`
	PackageUnits       []service.PackageUnitInput      `json:"package_units"`
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
		PackageUnits:       req.PackageUnits,
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

// CancelTakeout godoc
// @Summary      取消待支付外卖订单
// @Tags         用户-外卖
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "外卖单 ID"
// @Success      200  {object}  response.Body
// @Router       /user/takeout-orders/{id}/cancel [post]
func (h *TakeoutHandler) Cancel(c *gin.Context) {
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
	if err := h.TakeoutSvc.Cancel(accountID, id); err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, nil)
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

// MerchantListTakeouts godoc
// @Summary      商家外卖订单列表
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        status  query  string  false  "preparing|fulfilling|completed|cancelled"
// @Success      200     {object}  response.Body{data=query.PageResult}
// @Router       /merchant/takeout-orders [get]
func (h *TakeoutHandler) MerchantList(c *gin.Context) {
	merchantID, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var page query.Page
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BadRequest(c, "分页参数无效")
		return
	}
	pageNum, pageSize, _ := page.Normalize()
	list, total, err := h.TakeoutSvc.ListForMerchant(*merchantID, pageNum, pageSize, c.Query("status"))
	if err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, &query.PageResult{List: list, Total: total, Page: pageNum, PageSize: pageSize})
}

// MerchantPrepareTakeout godoc
// @Summary      商家确认外卖出餐
// @Tags         商家端
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "外卖单 ID"
// @Success      200  {object}  response.Body{data=service.TakeoutView}
// @Router       /merchant/takeout-orders/{id}/prepare [post]
func (h *TakeoutHandler) MerchantPrepare(c *gin.Context) {
	merchantID, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	view, err := h.TakeoutSvc.ConfirmPrepared(*merchantID, id)
	if err != nil {
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, view)
}

type RejectTakeoutRequest struct {
	Reason string `json:"reason" binding:"required" example:"今日已打烊"`
}

// MerchantRejectTakeout godoc
// @Summary      商家拒绝外卖订单
// @Tags         商家端
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "外卖单 ID"
// @Param        body  body  RejectTakeoutRequest  true  "拒绝原因"
// @Success      200   {object}  response.Body{data=service.TakeoutView}
// @Router       /merchant/takeout-orders/{id}/reject [post]
func (h *TakeoutHandler) MerchantReject(c *gin.Context) {
	merchantID, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req RejectTakeoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效: "+err.Error())
		return
	}
	view, err := h.TakeoutSvc.Reject(*merchantID, id, req.Reason)
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
	case errors.Is(err, service.ErrTakeoutForbidden):
		response.Fail(c, 403, 403, "无权操作该外卖订单")
	case errors.Is(err, service.ErrTakeoutStatusInvalid):
		msg := err.Error()
		if i := len(service.ErrTakeoutStatusInvalid.Error()); i+2 < len(msg) && msg[i:i+2] == ": " {
			response.BadRequest(c, msg[i+2:])
		} else {
			response.BadRequest(c, "当前状态不允许此操作")
		}
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
	case errors.Is(err, service.ErrVirtualNotDeliverable):
		response.BadRequest(c, "虚拟商品不支持配送")
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
