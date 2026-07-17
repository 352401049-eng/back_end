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

type UserHandler struct {
	Svc          *service.UserService
	RiderSvc     *service.RiderApplicationService
	AddressSvc   *service.AddressService
	CartSvc      *service.CartService
	OrderSvc     *service.OrderService
	InventorySvc *service.InventoryService
	DeliverySvc  *service.DeliveryService
}

// Overview godoc
// @Summary      个人中心概览
// @Description  返回资料摘要与各项数量统计（订单/配送/背包/库存使用角标）
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.OverviewResponse}
// @Failure      401  {object}  response.Body
// @Router       /user/overview [get]
func (h *UserHandler) Overview(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	data, err := h.Svc.GetOverview(accountID)
	if err != nil {
		response.InternalError(c, "获取个人概览失败")
		return
	}
	response.OK(c, data)
}

// Profile godoc
// @Summary      完整个人信息
// @Description  返回账号、扩展资料、地址列表与统计
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.ProfileDetail}
// @Failure      401  {object}  response.Body
// @Router       /user/profile [get]
func (h *UserHandler) Profile(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	data, err := h.Svc.GetProfile(accountID)
	if err != nil {
		response.InternalError(c, "获取个人信息失败")
		return
	}
	response.OK(c, data)
}

// Orders godoc
// @Summary      历史订单列表
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"  default(1)
// @Param        page_size  query  int  false  "每页条数"  default(10)
// @Param        status_code  query  string  false  "pending_group|pending_merchant|approved|..."
// @Param        status     query  int  false  "订单状态筛选"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Failure      400  {object}  response.Body
// @Failure      401  {object}  response.Body
// @Router       /user/orders [get]
func (h *UserHandler) Orders(c *gin.Context) {
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
	list, total, err := h.OrderSvc.List(accountID, nil, pageNum, pageSize, status, c.Query("status_code"), nil)
	if err != nil {
		response.InternalError(c, "获取订单列表失败")
		return
	}
	response.OK(c, &query.PageResult{List: list, Total: total, Page: pageNum, PageSize: pageSize})
}

// OrderDetail godoc
// @Summary      订单详情
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      int  true  "订单 ID"
// @Success      200  {object}  response.Body{data=service.OrderView}
// @Failure      400  {object}  response.Body
// @Failure      401  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /user/orders/{id} [get]
func (h *UserHandler) OrderDetail(c *gin.Context) {
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
	view, err := h.OrderSvc.GetView(accountID, orderID, nil)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			response.Fail(c, 404, 404, "订单不存在")
			return
		}
		response.InternalError(c, "获取订单详情失败")
		return
	}
	response.OK(c, view)
}

// Cart godoc
// @Summary      购物车
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=[]service.CartItemView}
// @Failure      401  {object}  response.Body
// @Router       /user/cart [get]
func (h *UserHandler) Cart(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	data, err := h.Svc.ListCart(accountID)
	if err != nil {
		response.InternalError(c, "获取购物车失败")
		return
	}
	response.OK(c, data)
}

// Coupons godoc
// @Summary      优惠券列表
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Param        status  query  int  false  "优惠券状态，0=未使用"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      401  {object}  response.Body
// @Router       /user/coupons [get]
func (h *UserHandler) Coupons(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
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
	data, err := h.Svc.ListCoupons(accountID, status)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}
	response.OK(c, data)
}

// Inventory godoc
// @Summary      背包库存
// @Description  购买成功后商品会进入背包；使用 POST /user/inventory/{id}/use
// @Tags         用户
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body
// @Failure      401  {object}  response.Body
// @Router       /user/inventory [get]
func (h *UserHandler) Inventory(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	data, err := h.Svc.ListInventory(accountID)
	if err != nil {
		response.InternalError(c, "获取背包失败")
		return
	}
	response.OK(c, data)
}
