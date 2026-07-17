package handler

import (
	"errors"
	"strconv"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AnnouncementHandler struct {
	AnnouncementSvc *service.AnnouncementService
	MerchantSvc     *service.MerchantService
}

type AnnouncementRequest struct {
	MerchantID uint64     `json:"merchant_id" example:"0"`
	Title      string     `json:"title" binding:"required"`
	Content    string     `json:"content" binding:"required"`
	CoverURL   *string    `json:"cover_url"`
	SortOrder  int        `json:"sort_order"`
	Status     uint8      `json:"status" example:"1"`
	PublishAt  *time.Time `json:"publish_at"`
	ExpireAt   *time.Time `json:"expire_at"`
}

// ListByMerchantPublic godoc
// @Summary      店铺公告列表
// @Tags         用户-商城
// @Produce      json
// @Param        id         path  int  true  "商家 ID"
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchants/{id}/announcements [get]
func (h *AnnouncementHandler) ListByMerchantPublic(c *gin.Context) {
	merchantID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "商家 ID 无效")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.AnnouncementSvc.List(page, pageSize, service.AnnouncementListFilter{
		MerchantID: &merchantID, PublicOnly: true,
	})
	if err != nil {
		response.InternalError(c, "获取公告失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListPublicAnnouncements godoc
// @Summary      公告列表（公开）
// @Description  merchant_id=0 或不传为平台公告；传商家 ID 为店铺公告
// @Tags         用户-商城
// @Produce      json
// @Param        merchant_id  query  int  false  "商家 ID，0=平台"
// @Param        page         query  int  false  "页码"
// @Param        page_size    query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /announcements [get]
func (h *AnnouncementHandler) ListPublic(c *gin.Context) {
	page, pageSize := parsePage(c)
	var merchantID uint64
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		merchantID = id
	}
	list, total, err := h.AnnouncementSvc.List(page, pageSize, service.AnnouncementListFilter{
		MerchantID: &merchantID, PublicOnly: true,
	})
	if err != nil {
		response.InternalError(c, "获取公告失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListMerchantAnnouncements godoc
// @Summary      本店公告列表
// @Tags         商家端-公告
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "管理员指定商家"
// @Param        page         query  int  false  "页码"
// @Param        page_size    query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /merchant/announcements [get]
func (h *AnnouncementHandler) ListMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.AnnouncementSvc.List(page, pageSize, service.AnnouncementListFilter{
		MerchantID: scope,
	})
	if err != nil {
		response.InternalError(c, "获取公告失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CreateMerchantAnnouncement godoc
// @Summary      创建公告
// @Tags         商家端-公告
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  AnnouncementRequest  true  "公告内容"
// @Success      200   {object}  response.Body{data=model.Announcement}
// @Router       /merchant/announcements [post]
func (h *AnnouncementHandler) CreateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	ann, err := h.AnnouncementSvc.Create(service.AnnouncementInput{
		MerchantID: *scope, Title: req.Title, Content: req.Content,
		CoverURL: req.CoverURL, SortOrder: req.SortOrder, Status: req.Status,
		PublishAt: req.PublishAt, ExpireAt: req.ExpireAt,
	})
	if err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, ann)
}

// UpdateMerchantAnnouncement godoc
// @Summary      更新公告
// @Tags         商家端-公告
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "公告 ID"
// @Param        body  body  AnnouncementRequest  true  "公告内容"
// @Success      200   {object}  response.Body{data=model.Announcement}
// @Router       /merchant/announcements/{id} [patch]
func (h *AnnouncementHandler) UpdateMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	ann, err := h.AnnouncementSvc.Update(id, service.AnnouncementInput{
		MerchantID: *scope, Title: req.Title, Content: req.Content,
		CoverURL: req.CoverURL, SortOrder: req.SortOrder, Status: req.Status,
		PublishAt: req.PublishAt, ExpireAt: req.ExpireAt,
	}, scope)
	if err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, ann)
}

// DeleteMerchantAnnouncement godoc
// @Summary      删除公告
// @Tags         商家端-公告
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "公告 ID"
// @Success      200  {object}  response.Body
// @Router       /merchant/announcements/{id} [delete]
func (h *AnnouncementHandler) DeleteMerchant(c *gin.Context) {
	scope, err := resolveMerchantScope(c, h.MerchantSvc)
	if err != nil {
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.AnnouncementSvc.Delete(id, scope); err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, nil)
}

// ListAdminAnnouncements godoc
// @Summary      公告列表（管理端）
// @Tags         管理端-公告
// @Produce      json
// @Security     BearerAuth
// @Param        merchant_id  query  int  false  "按商家筛选"
// @Param        page         query  int  false  "页码"
// @Param        page_size    query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/announcements [get]
func (h *AnnouncementHandler) ListAdmin(c *gin.Context) {
	page, pageSize := parsePage(c)
	filter := service.AnnouncementListFilter{}
	if raw := c.Query("merchant_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return
		}
		filter.MerchantID = &id
	}
	list, total, err := h.AnnouncementSvc.List(page, pageSize, filter)
	if err != nil {
		response.InternalError(c, "获取公告失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CreateAdminAnnouncement godoc
// @Summary      创建公告（平台或指定商家）
// @Tags         管理端-公告
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  AnnouncementRequest  true  "merchant_id=0 为平台公告"
// @Success      200   {object}  response.Body{data=model.Announcement}
// @Router       /admin/announcements [post]
func (h *AnnouncementHandler) CreateAdmin(c *gin.Context) {
	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	ann, err := h.AnnouncementSvc.Create(service.AnnouncementInput{
		MerchantID: req.MerchantID, Title: req.Title, Content: req.Content,
		CoverURL: req.CoverURL, SortOrder: req.SortOrder, Status: req.Status,
		PublishAt: req.PublishAt, ExpireAt: req.ExpireAt,
	})
	if err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, ann)
}

// UpdateAdminAnnouncement godoc
// @Summary      更新公告
// @Tags         管理端-公告
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "公告 ID"
// @Param        body  body  AnnouncementRequest  true  "公告内容"
// @Success      200   {object}  response.Body{data=model.Announcement}
// @Router       /admin/announcements/{id} [patch]
func (h *AnnouncementHandler) UpdateAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	ann, err := h.AnnouncementSvc.Update(id, service.AnnouncementInput{
		MerchantID: req.MerchantID, Title: req.Title, Content: req.Content,
		CoverURL: req.CoverURL, SortOrder: req.SortOrder, Status: req.Status,
		PublishAt: req.PublishAt, ExpireAt: req.ExpireAt,
	}, nil)
	if err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, ann)
}

// DeleteAdminAnnouncement godoc
// @Summary      删除公告
// @Tags         管理端-公告
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "公告 ID"
// @Success      200  {object}  response.Body
// @Router       /admin/announcements/{id} [delete]
func (h *AnnouncementHandler) DeleteAdmin(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.AnnouncementSvc.Delete(id, nil); err != nil {
		handleAnnouncementError(c, err)
		return
	}
	response.OK(c, nil)
}

func handleAnnouncementError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAnnouncementNotFound):
		response.Fail(c, 404, 404, "公告不存在")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, "参数无效")
	default:
		response.InternalError(c, "操作失败")
	}
}

var _ model.Announcement
