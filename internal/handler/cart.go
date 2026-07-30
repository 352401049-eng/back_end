package handler

import (
	"errors"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AddCartRequest struct {
	ProductID      uint64  `json:"product_id" binding:"required"`
	Quantity       uint32  `json:"quantity" example:"1"`
	Spec           *string `json:"spec"`
	PurchaseType   uint8   `json:"purchase_type" example:"1"`
	GroupBuyID     *uint64 `json:"group_buy_id"`
	GroupBuyTeamID *uint64 `json:"group_buy_team_id"`
}

type UpdateCartRequest struct {
	Quantity *uint32 `json:"quantity"`
	Selected *uint8  `json:"selected"`
}

// AddCart godoc
// @Summary      加入购物车
// @Tags         用户-购物车
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  AddCartRequest  true  "加购信息"
// @Success      200   {object}  response.Body{data=model.CartItem}
// @Router       /user/cart [post]
func (h *UserHandler) AddCart(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req AddCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	item, err := h.CartSvc.Add(accountID, service.AddCartInput{
		ProductID: req.ProductID, Quantity: req.Quantity, Spec: req.Spec,
		PurchaseType: req.PurchaseType, GroupBuyID: req.GroupBuyID, GroupBuyTeamID: req.GroupBuyTeamID,
	})
	if err != nil {
		handleCartError(c, err)
		return
	}
	response.OK(c, item)
}

// UpdateCart godoc
// @Summary      更新购物车项
// @Tags         用户-购物车
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int               true  "购物车项 ID"
// @Param        body  body  UpdateCartRequest true  "更新内容"
// @Success      200   {object}  response.Body{data=model.CartItem}
// @Router       /user/cart/{id} [patch]
func (h *UserHandler) UpdateCart(c *gin.Context) {
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
	var req UpdateCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	item, err := h.CartSvc.Update(accountID, id, service.UpdateCartInput{Quantity: req.Quantity, Selected: req.Selected})
	if err != nil {
		handleCartError(c, err)
		return
	}
	response.OK(c, item)
}

// DeleteCart godoc
// @Summary      删除购物车项
// @Tags         用户-购物车
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "购物车项 ID"
// @Success      200  {object}  response.Body
// @Router       /user/cart/{id} [delete]
func (h *UserHandler) DeleteCart(c *gin.Context) {
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
	if err := h.CartSvc.Delete(accountID, id); err != nil {
		handleCartError(c, err)
		return
	}
	response.OK(c, nil)
}

type CheckoutCartRequest struct {
	CartItemIDs       []uint64 `json:"cart_item_ids" binding:"required,min=1"`
	MerchantID        uint64   `json:"merchant_id" binding:"required"`
	DeliveryType      uint8    `json:"delivery_type"`
	AddressID         *uint64  `json:"address_id"`
	DeliveryLatitude  *float64 `json:"delivery_latitude"`
	DeliveryLongitude *float64 `json:"delivery_longitude"`
	Remark            *string  `json:"remark"`
	UserCouponID      *uint64  `json:"user_coupon_id"`
}

// CheckoutCart godoc
// @Summary      购物车合单结算（已废弃）
// @Tags         用户-购物车
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CheckoutCartRequest  true  "合单信息"
// @Failure      400   {object}  response.Body
// @Router       /user/cart/checkout [post]
func (h *UserHandler) CheckoutCart(c *gin.Context) {
	_, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CheckoutCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	_ = req
	response.BadRequest(c, "请使用外卖结算")
}

type CheckoutCartTakeoutRequest struct {
	CartItemIDs        []uint64                              `json:"cart_item_ids" binding:"required,min=1"`
	MerchantID         uint64                                `json:"merchant_id" binding:"required"`
	AddressID          uint64                                `json:"address_id" binding:"required"`
	DeliveryTimeRemark string                                `json:"delivery_time_remark"`
	Lines              []service.CartCheckoutTakeoutLineInput `json:"lines"`
}

// CheckoutCartTakeout godoc
// @Summary      购物车外卖合单结算
// @Tags         用户-购物车
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CheckoutCartTakeoutRequest  true  "外卖合单信息"
// @Success      200   {object}  response.Body{data=service.TakeoutView}
// @Router       /user/cart/takeout-checkout [post]
func (h *UserHandler) CheckoutCartTakeout(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CheckoutCartTakeoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if h.TakeoutSvc == nil {
		response.InternalError(c, "服务未就绪")
		return
	}
	view, err := h.TakeoutSvc.CreateFromCart(accountID, service.CartCheckoutTakeoutInput{
		CartItemIDs: req.CartItemIDs, MerchantID: req.MerchantID,
		AddressID: req.AddressID, DeliveryTimeRemark: req.DeliveryTimeRemark,
		Lines: req.Lines,
	})
	if err != nil {
		if errors.Is(err, service.ErrCartItemNotFound) {
			response.Fail(c, 404, 404, "购物车项不存在")
			return
		}
		handleTakeoutError(c, err)
		return
	}
	response.OK(c, view)
}

func handleCartError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCartItemNotFound):
		response.Fail(c, 404, 404, "购物车项不存在")
	case errors.Is(err, service.ErrCartProductInvalid):
		response.BadRequest(c, "商品无效或未上架")
	case errors.Is(err, service.ErrInvalidProductArg):
		msg := err.Error()
		if i := len(service.ErrInvalidProductArg.Error()); i+2 < len(msg) && msg[i:i+2] == ": " {
			response.BadRequest(c, msg[i+2:])
		} else {
			response.BadRequest(c, "商品无效或未上架")
		}
	case errors.Is(err, service.ErrDeliveryNotAllowed), errors.Is(err, service.ErrVirtualNotDeliverable):
		response.BadRequest(c, "该商品不支持外卖配送")
	default:
		response.InternalError(c, "操作失败")
	}
}

var _ model.CartItem
