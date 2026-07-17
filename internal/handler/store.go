package handler

import (
	"errors"
	"strconv"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type StoreHandler struct {
	MerchantSvc *service.MerchantService
	ProductSvc  *service.ProductService
	OrderSvc    *service.OrderService
	CategorySvc *service.CategoryService
	CouponSvc   *service.CouponService
	ActivitySvc *service.ActivityService
	ZoneSvc     *service.DeliveryZoneService
}

// ListMerchantCategories godoc
// @Summary      商家商品分类
// @Description  返回某营业中店铺自有的商品分类（仅 status=1），供外部按商家+分类筛选商品
// @Tags         用户-商城
// @Produce      json
// @Param        id   path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=[]model.ProductCategory}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id}/categories [get]
func (h *StoreHandler) ListMerchantCategories(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	if _, err := h.MerchantSvc.GetOpenByID(merchantID); err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	list, err := h.CategorySvc.ListByMerchant(merchantID, true)
	if err != nil {
		response.InternalError(c, "获取分类失败")
		return
	}
	response.OK(c, list)
}

// ListMerchants godoc
// @Summary      商家列表
// @Description  展示营业中的商家，无需登录
// @Tags         用户-商城
// @Produce      json
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Param        keyword    query  string  false  "店铺名/地址搜索"
// @Success      200  {object}  response.Body{data=query.PageResult{list=[]service.ProductStoreView}}
// @Router       /merchants [get]
func (h *StoreHandler) ListMerchants(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.MerchantSvc.ListOpen(page, pageSize, c.Query("keyword"))
	if err != nil {
		response.InternalError(c, "获取商家列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetMerchant godoc
// @Summary      商家详情
// @Description  查看营业中商家信息
// @Tags         用户-商城
// @Produce      json
// @Param        id   path  int  true  "商家 ID"
// @Success      200  {object}  response.Body{data=MerchantProfileResp}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id} [get]
func (h *StoreHandler) GetMerchant(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	profile, err := h.MerchantSvc.GetOpenByID(id)
	if err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	response.OK(c, profile)
}

// ListMerchantProducts godoc
// @Summary      商家商品列表（路径传 merchant_id）
// @Description  某商家已上架商品，支持分类与关键词筛选
// @Tags         用户-商城
// @Produce      json
// @Param        id           path   int     true   "商家 ID"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        category_id  query  int     false  "分类 ID"
// @Param        keyword      query  string  false  "商品名"
// @Success      200  {object}  response.Body{data=query.PageResult{list=[]service.ProductStoreView}}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id}/products [get]
func (h *StoreHandler) ListMerchantProducts(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	h.listMerchantProducts(c, merchantID)
}

// ListProductsByMerchant godoc
// @Summary      按商家 ID 查询商品
// @Description  根据 merchant_id 返回该商家已上架商品；与 GET /merchants/{id}/products 等价
// @Tags         用户-商城
// @Produce      json
// @Param        merchant_id  query  int     true   "商家 ID"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页条数"
// @Param        category_id  query  int     false  "分类 ID"
// @Param        keyword      query  string  false  "商品名"
// @Success      200  {object}  response.Body{data=query.PageResult{list=[]service.ProductStoreView}}
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /products [get]
func (h *StoreHandler) ListProductsByMerchant(c *gin.Context) {
	s := c.Query("merchant_id")
	if s == "" {
		response.BadRequest(c, "请指定 merchant_id")
		return
	}
	merchantID, err := parseUintParamFromString(s)
	if err != nil {
		response.BadRequest(c, "merchant_id 无效")
		return
	}
	h.listMerchantProducts(c, merchantID)
}

func (h *StoreHandler) listMerchantProducts(c *gin.Context, merchantID uint64) {
	page, pageSize := parsePage(c)
	filter := service.ProductListFilter{Keyword: c.Query("keyword")}
	if c.Query("group_buy") == "1" {
		filter.EnableGroupBuyOnly = true
	}
	if s := c.Query("category_id"); s != "" {
		v, parseErr := parseUintParamFromString(s)
		if parseErr != nil {
			response.BadRequest(c, "category_id 参数无效")
			return
		}
		filter.CategoryID = &v
	}
	list, total, err := h.ProductSvc.ListOnShelfByMerchant(merchantID, page, pageSize, filter)
	if err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		if errors.Is(err, service.ErrCategoryNotFound) {
			response.Fail(c, 404, 404, "分类不存在")
			return
		}
		if errors.Is(err, service.ErrCategoryForbidden) {
			response.BadRequest(c, "分类不属于该商家")
			return
		}
		response.InternalError(c, "获取商品列表失败")
		return
	}
	response.OK(c, query.PageResult{
		List:     h.ProductSvc.ToStoreViews(list),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetMerchantProduct godoc
// @Summary      商品详情
// @Description  某商家下的上架商品详情
// @Tags         用户-商城
// @Produce      json
// @Param        id          path  int  true  "商家 ID"
// @Param        product_id  path  int  true  "商品 ID"
// @Success      200  {object}  response.Body{data=service.ProductStoreView}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id}/products/{product_id} [get]
func (h *StoreHandler) GetMerchantProduct(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	productID, err := parseUintParam(c, "product_id")
	if err != nil {
		response.BadRequest(c, "商品 ID 无效")
		return
	}
	product, err := h.ProductSvc.GetOnShelf(productID, merchantID)
	if err != nil {
		if errors.Is(err, service.ErrProductNotFound) || errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商品不存在或已下架")
			return
		}
		response.InternalError(c, "获取商品失败")
		return
	}
	response.OK(c, h.ProductSvc.ToStoreView(product))
}

func optionalAccountID(c *gin.Context) *uint64 {
	if id, ok := auth.AccountID(c); ok {
		return &id
	}
	return nil
}

// ListMerchantCoupons godoc
// @Summary      商家可领取优惠券
// @Description  返回该商家券及可选平台通用券；登录后返回 claimed/can_claim
// @Tags         用户-商城
// @Produce      json
// @Param        id                path   int   true   "商家 ID"
// @Param        include_platform  query  bool  false  "是否包含平台通用券，默认 true"
// @Success      200  {object}  response.Body{data=[]service.ClaimableCouponView}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id}/coupons [get]
func (h *StoreHandler) ListMerchantCoupons(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	if _, err := h.MerchantSvc.GetOpenByID(merchantID); err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	includePlatform := c.Query("include_platform") != "0"
	list, err := h.CouponSvc.ListClaimableByMerchant(merchantID, optionalAccountID(c), includePlatform)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}
	response.OK(c, list)
}

// GetMerchantStore godoc
// @Summary      商家店铺聚合页
// @Description  一次返回进行中的限时活动、可领优惠券、团购商品
// @Tags         用户-商城
// @Produce      json
// @Param        id                path   int   true   "商家 ID"
// @Param        include_platform  query  bool  false  "优惠券是否含平台通用券"
// @Success      200  {object}  response.Body{data=service.MerchantStoreOverview}
// @Failure      404  {object}  response.Body
// @Router       /merchants/{id}/store [get]
func (h *StoreHandler) GetMerchantStore(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	merchant, err := h.MerchantSvc.GetOpenByID(merchantID)
	if err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家不存在或已停业")
			return
		}
		response.InternalError(c, "获取商家失败")
		return
	}
	includePlatform := c.Query("include_platform") != "0"

	activities, _, err := h.ActivitySvc.List(1, 20, service.ActivityListFilter{
		MerchantID: &merchantID, ActiveOnly: true,
	})
	if err != nil {
		response.InternalError(c, "获取活动失败")
		return
	}

	coupons, err := h.CouponSvc.ListClaimableByMerchant(merchantID, optionalAccountID(c), includePlatform)
	if err != nil {
		response.InternalError(c, "获取优惠券失败")
		return
	}

	groupProducts, _, err := h.ProductSvc.ListOnShelfByMerchant(merchantID, 1, 20, service.ProductListFilter{
		EnableGroupBuyOnly: true,
	})
	if err != nil {
		response.InternalError(c, "获取团购商品失败")
		return
	}

	var deliveryZone *service.DeliveryZoneView
	if h.ZoneSvc != nil {
		deliveryZone, _ = h.ZoneSvc.GetPublicView(merchantID)
	}

	response.OK(c, service.MerchantStoreOverview{
		Merchant:         *merchant,
		DeliveryZone:     deliveryZone,
		ActiveActivities: h.ActivitySvc.ToStoreViews(activities, true),
		ClaimableCoupons: coupons,
		GroupBuyProducts: h.ProductSvc.ToStoreViews(groupProducts),
	})
}

// GetActivityGroupProgress godoc
// @Summary      活动商品拼团进度
// @Tags         用户-商城
// @Produce      json
// @Param        id                  path   int  true  "活动 ID"
// @Param        activity_product_id path   int  true  "活动商品 ID"
// @Param        team_id             query  int  false "指定团 ID"
// @Success      200  {object}  response.Body{data=service.GroupBuyProgress}
// @Router       /activities/{id}/products/{activity_product_id}/group [get]
func (h *StoreHandler) GetActivityGroupProgress(c *gin.Context) {
	activityID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "活动 ID 无效")
		return
	}
	apID, err := parseUintParam(c, "activity_product_id")
	if err != nil {
		response.BadRequest(c, "活动商品 ID 无效")
		return
	}
	var accountID uint64
	if id, ok := auth.AccountID(c); ok {
		accountID = id
	}
	var teamID *uint64
	if s := c.Query("team_id"); s != "" {
		v, parseErr := parseUintParamFromString(s)
		if parseErr != nil {
			response.BadRequest(c, "team_id 无效")
			return
		}
		teamID = &v
	}
	progress, err := h.OrderSvc.GetActivityGroupProgress(accountID, activityID, apID, teamID)
	if err != nil {
		handleStoreGroupError(c, err)
		return
	}
	response.OK(c, progress)
}

func handleStoreGroupError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrProductNotFound), errors.Is(err, service.ErrActivityNotFound),
		errors.Is(err, service.ErrActivityProductNotFound):
		response.Fail(c, 404, 404, "活动商品不存在")
	case errors.Is(err, service.ErrActivityNotActive):
		response.BadRequest(c, "活动未开始或已结束")
	case errors.Is(err, service.ErrGroupBuyInvalid):
		response.BadRequest(c, "该商品不支持拼团")
	default:
		response.InternalError(c, "获取拼团进度失败")
	}
}

func parseUintParamFromString(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}

var _ model.ProductCategory
