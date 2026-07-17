package handler

import (
	"errors"
	"strconv"
	"time"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type CouponHandler struct {
	CouponSvc   *service.CouponService
	MerchantSvc *service.MerchantService
	ProductSvc  *service.ProductService
}

type CreateCouponRequest struct {
	Name           string    `json:"name" binding:"required" example:"满100减10"`
	Type           uint8     `json:"type" binding:"required" example:"1"`
	MerchantID     *uint64   `json:"merchant_id" example:"1"`
	MinAmount      float64   `json:"min_amount" example:"100"`
	DiscountAmount *float64  `json:"discount_amount" example:"10"`
	DiscountRate   *uint8    `json:"discount_rate" example:"85"`
	MaxDiscount    *float64  `json:"max_discount" example:"50"`
	TotalQuota     uint32    `json:"total_quota" example:"1000"`
	ScopeType      uint8     `json:"scope_type" example:"0"`
	ScopeIDs       []uint64  `json:"scope_ids"`
	StartAt        time.Time `json:"start_at" binding:"required"`
	EndAt          time.Time `json:"end_at" binding:"required"`
}

type UpdateCouponRequest struct {
	Name           *string    `json:"name"`
	MinAmount      *float64   `json:"min_amount"`
	DiscountAmount *float64   `json:"discount_amount"`
	DiscountRate   *uint8     `json:"discount_rate"`
	MaxDiscount    *float64   `json:"max_discount"`
	TotalQuota     *uint32    `json:"total_quota"`
	ScopeType      *uint8     `json:"scope_type"`
	ScopeIDs       *[]uint64  `json:"scope_ids"`
	StartAt        *time.Time `json:"start_at"`
	EndAt          *time.Time `json:"end_at"`
}

type UpdateCouponStatusRequest struct {
	Status *uint8 `json:"status" binding:"required,oneof=0 1" example:"1"`
}

type ClaimCouponRequest struct {
	CouponID uint64 `json:"coupon_id" binding:"required" example:"1"`
}

// ListPublicCoupons godoc
// @Summary      可领取优惠券列表
// @Description  平台券或指定商家券；登录后返回 claimed/can_claim
// @Tags         优惠券
// @Produce      json
// @Param        merchant_id  query  int  false  "商家 ID，不传则仅平台券"
// @Success      200  {object}  response.Body{data=[]service.ClaimableCouponView}
// @Router       /coupons [get]
func (h *CouponHandler) ListPublic(c *gin.Context) {
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
	}
	var accountID *uint64
	if id, ok := auth.AccountID(c); ok {
		accountID = &id
	}
	list, err := h.CouponSvc.ListClaimable(merchantID, accountID)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}
	response.OK(c, list)
}

// ClaimCoupon godoc
// @Summary      领取优惠券
// @Tags         用户-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ClaimCouponRequest  true  "券模板 ID"
// @Success      200   {object}  response.Body{data=model.UserCoupon}
// @Router       /user/coupons/claim [post]
func (h *CouponHandler) Claim(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req ClaimCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	uc, err := h.CouponSvc.Claim(accountID, req.CouponID)
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, uc)
}

// ListApplicableCoupons godoc
// @Summary      下单可用优惠券
// @Description  根据商品与金额筛选当前用户可用券，并预估优惠
// @Tags         用户-优惠券
// @Produce      json
// @Security     BearerAuth
// @Param        product_id     query  int  true  "商品 ID"
// @Param        merchant_id    query  int  true  "商家 ID"
// @Param        quantity       query  int  false  "数量"
// @Param        purchase_type  query  int  false  "1=直购 2=拼团"
// @Success      200  {object}  response.Body
// @Router       /user/coupons/applicable [get]
func (h *CouponHandler) ListApplicable(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	ctx, err := h.buildOrderCouponContext(c, accountID)
	if err != nil {
		return
	}
	list, err := h.CouponSvc.ListApplicable(accountID, ctx)
	if err != nil {
		response.InternalError(c, "获取可用优惠券失败")
		return
	}
	response.OK(c, list)
}

// ListAdminCoupons godoc
// @Summary      优惠券列表（管理端）
// @Tags         管理端-优惠券
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Param        status     query  int  false  "0/1"
// @Param        merchant_id  query  int  false  "按商家筛选"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/coupons [get]
func (h *CouponHandler) ListAdmin(c *gin.Context) {
	page, pageSize := parsePage(c)
	var merchantScope *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantScope = &id
	}
	var status *uint8
	if raw := c.Query("status"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 8)
		if err != nil {
			response.BadRequest(c, "status 无效")
			return
		}
		u := uint8(v)
		status = &u
	}
	list, total, err := h.CouponSvc.List(page, pageSize, merchantScope, status)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CreateAdminCoupon godoc
// @Summary      创建优惠券（管理端）
// @Tags         管理端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateCouponRequest  true  "券信息"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /admin/coupons [post]
func (h *CouponHandler) CreateAdmin(c *gin.Context) {
	var req CreateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	coupon, err := h.CouponSvc.Create(toCreateCouponInput(req))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, coupon)
}

// GetAdminCoupon godoc
// @Summary      优惠券详情（管理端）
// @Tags         管理端-优惠券
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "券 ID"
// @Success      200  {object}  response.Body{data=model.Coupon}
// @Router       /admin/coupons/{id} [get]
func (h *CouponHandler) GetAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	coupon, err := h.CouponSvc.GetByID(id)
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, coupon)
}

// UpdateAdminCoupon godoc
// @Summary      更新优惠券（管理端）
// @Tags         管理端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "券 ID"
// @Param        body  body  UpdateCouponRequest  true  "更新字段"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /admin/coupons/{id} [patch]
func (h *CouponHandler) UpdateAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	coupon, err := h.CouponSvc.Update(id, nil, toUpdateCouponInput(req))
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, coupon)
}

// UpdateAdminCouponStatus godoc
// @Summary      启用/停用优惠券（管理端）
// @Tags         管理端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "券 ID"
// @Param        body  body  UpdateCouponStatusRequest   true  "状态"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /admin/coupons/{id}/status [patch]
func (h *CouponHandler) UpdateAdminStatus(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCouponStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	coupon, err := h.CouponSvc.UpdateStatus(id, nil, *req.Status)
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, coupon)
}

// ListMerchantCoupons godoc
// @Summary      本店优惠券列表
// @Tags         商家端-优惠券
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员代管时指定"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/coupons [get]
func (h *CouponHandler) ListMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	var status *uint8
	if raw := c.Query("status"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 8)
		if err == nil {
			u := uint8(v)
			status = &u
		}
	}
	list, total, err := h.CouponSvc.List(page, pageSize, scope, status)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CreateMerchantCoupon godoc
// @Summary      创建本店优惠券
// @Tags         商家端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateCouponRequest  true  "券信息"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /merchant/coupons [post]
func (h *CouponHandler) CreateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var req CreateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	input := toCreateCouponInput(req)
	input.MerchantID = scope
	coupon, err := h.CouponSvc.Create(input)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, coupon)
}

// GetMerchantCoupon godoc
// @Summary      优惠券详情（商家端）
// @Tags         商家端-优惠券
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "券 ID"
// @Success      200  {object}  response.Body{data=model.Coupon}
// @Router       /merchant/coupons/{id} [get]
func (h *CouponHandler) GetMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	coupon, err := h.CouponSvc.GetByID(id)
	if err != nil {
		handleCouponError(c, err)
		return
	}
	if coupon.MerchantID == nil || *coupon.MerchantID != *scope {
		response.Fail(c, 404, 404, "优惠券不存在")
		return
	}
	response.OK(c, coupon)
}

// UpdateMerchantCoupon godoc
// @Summary      更新本店优惠券
// @Tags         商家端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "券 ID"
// @Param        body  body  UpdateCouponRequest  true  "更新字段"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /merchant/coupons/{id} [patch]
func (h *CouponHandler) UpdateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	coupon, err := h.CouponSvc.Update(id, scope, toUpdateCouponInput(req))
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, coupon)
}

// UpdateMerchantCouponStatus godoc
// @Summary      启用/停用本店优惠券
// @Tags         商家端-优惠券
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                         true  "券 ID"
// @Param        body  body  UpdateCouponStatusRequest   true  "状态"
// @Success      200   {object}  response.Body{data=model.Coupon}
// @Router       /merchant/coupons/{id}/status [patch]
func (h *CouponHandler) UpdateMerchantStatus(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCouponStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	coupon, err := h.CouponSvc.UpdateStatus(id, scope, *req.Status)
	if err != nil {
		handleCouponError(c, err)
		return
	}
	response.OK(c, coupon)
}

func (h *CouponHandler) buildOrderCouponContext(c *gin.Context, accountID uint64) (service.OrderCouponContext, error) {
	productID, err := strconv.ParseUint(c.Query("product_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "product_id 无效")
		return service.OrderCouponContext{}, err
	}
	merchantID, err := strconv.ParseUint(c.Query("merchant_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "merchant_id 无效")
		return service.OrderCouponContext{}, err
	}
	qty := uint32(1)
	if raw := c.Query("quantity"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 32)
		if err == nil && v > 0 {
			qty = uint32(v)
		}
	}
	purchaseType := uint8(model.PurchaseTypeSolo)
	if raw := c.Query("purchase_type"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 8)
		if err == nil {
			purchaseType = uint8(v)
		}
	}
	product, err := h.ProductSvc.GetByID(productID, nil)
	if err != nil {
		if errors.Is(err, service.ErrProductNotFound) {
			response.Fail(c, 404, 404, "商品不存在")
		} else {
			response.InternalError(c, "获取商品失败")
		}
		return service.OrderCouponContext{}, err
	}
	if product.MerchantID != merchantID {
		response.BadRequest(c, "商品与商家不匹配")
		return service.OrderCouponContext{}, errors.New("merchant mismatch")
	}
	unitPrice := product.Price
	if purchaseType == model.PurchaseTypeGroup && product.GroupBuyPrice != nil {
		unitPrice = *product.GroupBuyPrice
	}
	return service.OrderCouponContext{
		AccountID: accountID, MerchantID: merchantID, Product: *product,
		Subtotal: unitPrice * float64(qty), PurchaseType: purchaseType,
	}, nil
}

func toCreateCouponInput(req CreateCouponRequest) service.CreateCouponInput {
	return service.CreateCouponInput{
		Name: req.Name, Type: req.Type, MerchantID: req.MerchantID,
		MinAmount: req.MinAmount, DiscountAmount: req.DiscountAmount,
		DiscountRate: req.DiscountRate, MaxDiscount: req.MaxDiscount,
		TotalQuota: req.TotalQuota, ScopeType: req.ScopeType, ScopeIDs: req.ScopeIDs,
		StartAt: req.StartAt, EndAt: req.EndAt,
	}
}

func toUpdateCouponInput(req UpdateCouponRequest) service.UpdateCouponInput {
	return service.UpdateCouponInput{
		Name: req.Name, MinAmount: req.MinAmount, DiscountAmount: req.DiscountAmount,
		DiscountRate: req.DiscountRate, MaxDiscount: req.MaxDiscount, TotalQuota: req.TotalQuota,
		ScopeType: req.ScopeType, ScopeIDs: req.ScopeIDs, StartAt: req.StartAt, EndAt: req.EndAt,
	}
}

func handleCouponError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCouponNotFound):
		response.Fail(c, 404, 404, "优惠券不存在")
	case errors.Is(err, service.ErrCouponUnavailable):
		response.BadRequest(c, "优惠券不可用")
	case errors.Is(err, service.ErrCouponQuotaExceeded):
		response.BadRequest(c, "优惠券已领完")
	case errors.Is(err, service.ErrCouponAlreadyClaimed):
		response.BadRequest(c, "您已领取过该优惠券")
	case errors.Is(err, service.ErrUserCouponNotFound):
		response.Fail(c, 404, 404, "用户优惠券不存在")
	case errors.Is(err, service.ErrUserCouponInvalid):
		response.BadRequest(c, "优惠券不可用或已过期")
	case errors.Is(err, service.ErrCouponNotApplicable):
		response.BadRequest(c, "优惠券不满足使用条件")
	default:
		response.InternalError(c, "操作失败")
	}
}
