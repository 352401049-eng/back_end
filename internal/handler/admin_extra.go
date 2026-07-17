package handler

import (
	"errors"
	"strconv"

	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminExtraHandler struct {
	UserSvc      *service.UserService
	CategorySvc  *service.CategoryService
	DeliverySvc  *service.DeliveryService
	InventorySvc *service.InventoryService
}

type CreateCategoryRequest struct {
	MerchantID uint64  `json:"merchant_id" example:"1"`
	ParentID   uint64  `json:"parent_id" example:"0"`
	Name       string  `json:"name" binding:"required" example:"生鲜"`
	IconURL    *string `json:"icon_url"`
	SortOrder  int     `json:"sort_order" example:"0"`
	Status     uint8   `json:"status" example:"1"`
}

type UpdateCategoryRequest struct {
	Name      *string `json:"name"`
	IconURL   *string `json:"icon_url"`
	SortOrder *int    `json:"sort_order"`
	Status    *uint8  `json:"status"`
}

// ListAdminUsers godoc
// @Summary      用户列表（管理端）
// @Tags         管理端-用户
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Param        keyword    query  string  false  "昵称/手机号"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/users [get]
func (h *AdminExtraHandler) ListUsers(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.UserSvc.ListUsersForAdmin(page, pageSize, c.Query("keyword"))
	if err != nil {
		response.InternalError(c, "获取用户列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListAdminCategories godoc
// @Summary      分类列表（管理端，含隐藏）
// @Tags         管理端-分类
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "按商家筛选"
// @Param        status       query  int  false  "0/1"
// @Success      200  {object}  response.Body
// @Router       /admin/categories [get]
func (h *AdminExtraHandler) ListCategories(c *gin.Context) {
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
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
	}
	list, err := h.CategorySvc.ListAllScoped(merchantID, status)
	if err != nil {
		response.InternalError(c, "获取分类失败")
		return
	}
	response.OK(c, list)
}

// CreateAdminCategory godoc
// @Summary      创建分类
// @Tags         管理端-分类
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  CreateCategoryRequest  true  "分类信息"
// @Success      200   {object}  response.Body
// @Router       /admin/categories [post]
func (h *AdminExtraHandler) CreateCategory(c *gin.Context) {
	var req CreateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if req.MerchantID == 0 {
		response.BadRequest(c, "请指定 merchant_id")
		return
	}
	cat, err := h.CategorySvc.Create(service.CreateCategoryInput{
		MerchantID: req.MerchantID,
		ParentID: req.ParentID, Name: req.Name, IconURL: req.IconURL,
		SortOrder: req.SortOrder, Status: req.Status,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, cat)
}

// GetAdminCategory godoc
// @Summary      分类详情
// @Tags         管理端-分类
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "分类 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/categories/{id} [get]
func (h *AdminExtraHandler) GetCategory(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	cat, err := h.CategorySvc.GetByID(id)
	if err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, cat)
}

// UpdateAdminCategory godoc
// @Summary      更新分类
// @Tags         管理端-分类
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                    true  "分类 ID"
// @Param        body  body  UpdateCategoryRequest  true  "更新字段"
// @Success      200   {object}  response.Body
// @Router       /admin/categories/{id} [patch]
func (h *AdminExtraHandler) UpdateCategory(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req UpdateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	cat, err := h.CategorySvc.Update(id, service.UpdateCategoryInput{
		Name: req.Name, IconURL: req.IconURL, SortOrder: req.SortOrder, Status: req.Status,
	})
	if err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, cat)
}

// DeleteAdminCategory godoc
// @Summary      删除分类（逻辑删除）
// @Tags         管理端-分类
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "分类 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/categories/{id} [delete]
func (h *AdminExtraHandler) DeleteCategory(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.CategorySvc.Delete(id); err != nil {
		handleCategoryError(c, err)
		return
	}
	response.OK(c, nil)
}

// ListAdminDeliveries godoc
// @Summary      配送单列表（管理端）
// @Tags         管理端-配送
// @Produce      json
// @Security     BearerAuth
// @Param        page        query  int  false  "页码"
// @Param        page_size   query  int  false  "每页条数"
// @Param        merchant_id query  int  false  "按商家筛选"
// @Param        status      query  int  false  "配送状态"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/deliveries [get]
func (h *AdminExtraHandler) ListDeliveries(c *gin.Context) {
	page, pageSize := parsePage(c)
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
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
	list, total, err := h.DeliverySvc.ListForAdmin(merchantID, status, page, pageSize)
	if err != nil {
		response.InternalError(c, "获取配送单失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListAdminInventoryUsages godoc
// @Summary      背包使用记录（管理端）
// @Tags         管理端-背包
// @Produce      json
// @Security     BearerAuth
// @Param        page        query  int  false  "页码"
// @Param        page_size   query  int  false  "每页条数"
// @Param        merchant_id query  int  false  "按商家筛选"
// @Param        status      query  int  false  "状态"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/inventory-usages [get]
func (h *AdminExtraHandler) ListInventoryUsages(c *gin.Context) {
	page, pageSize := parsePage(c)
	var merchantID *uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = &id
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
	list, total, err := h.InventorySvc.ListUsagesForAdmin(merchantID, status, page, pageSize)
	if err != nil {
		response.InternalError(c, "获取使用记录失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

func handleCategoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCategoryNotFound):
		response.Fail(c, 404, 404, "分类不存在")
	case errors.Is(err, service.ErrCategoryForbidden):
		response.Fail(c, 403, 403, "分类不属于该商家")
	default:
		response.BadRequest(c, err.Error())
	}
}
