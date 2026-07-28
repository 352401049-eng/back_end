package handler

import (
	"errors"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// ListRiders godoc
// @Summary      骑手列表（含收益汇总）
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        keyword    query  string  false  "昵称/手机号"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/riders [get]
func (h *AdminHandler) ListRiders(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListRiders(c.Query("keyword"), page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetRider godoc
// @Summary      骑手详情
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "骑手 account ID"
// @Success      200  {object}  response.Body{data=service.RiderOverview}
// @Router       /admin/riders/{id} [get]
func (h *AdminHandler) GetRider(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	ov, err := h.EarningSvc.AdminGetRider(id)
	if err != nil {
		if errors.Is(err, service.ErrRiderNotFound) {
			response.Fail(c, 404, 404, "骑手不存在")
			return
		}
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, ov)
}

// RevokeRider godoc
// @Summary      撤销骑手身份
// @Description  is_rider 置 0，历史收益保留
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "骑手 account ID"
// @Success      200  {object}  response.Body
// @Router       /admin/riders/{id}/revoke [patch]
func (h *AdminHandler) RevokeRider(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	if err := h.EarningSvc.AdminRevokeRider(id); err != nil {
		response.InternalError(c, "撤销失败")
		return
	}
	response.OK(c, nil)
}

// ListRiderEarnings godoc
// @Summary      某骑手的收益记录
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id         path  int  true  "骑手 account ID"
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/riders/{id}/earnings [get]
func (h *AdminHandler) ListRiderEarnings(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListRiderEarnings(id, page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListRiderDeliveries godoc
// @Summary      某骑手的送餐记录
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "骑手 account ID"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/riders/{id}/deliveries [get]
func (h *AdminHandler) ListRiderDeliveries(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	// 复用 DeliveryService.ListForRider 查询某骑手的送餐记录
	// scope=history 返回全部历史（含已完成）
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListRiderDeliveries(id, page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListRiderSettlements godoc
// @Summary      某骑手的结账记录
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "骑手 account ID"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/riders/{id}/settlements [get]
func (h *AdminHandler) ListRiderSettlements(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListRiderSettlements(id, page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CreateSettlement godoc
// @Summary      管理员主动结账
// @Description  输入金额，创建待审批结账单
// @Tags         管理端-骑手
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                          true  "骑手 account ID"
// @Param        body  body  CreateSettlementRequest      true  "结账信息"
// @Success      200  {object}  response.Body{data=model.RiderSettlement}
// @Router       /admin/riders/{id}/settlements [post]
func (h *AdminHandler) CreateSettlement(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	operatorID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req CreateSettlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	st, err := h.EarningSvc.AdminCreateSettlement(id, req.Amount, operatorID)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientEarnings) {
			response.BadRequest(c, "待结账金额不足")
			return
		}
		if errors.Is(err, service.ErrSettlementInvalid) {
			response.BadRequest(c, "结账金额无效")
			return
		}
		response.InternalError(c, "结账失败")
		return
	}
	response.OK(c, st)
}

type CreateSettlementRequest struct {
	Amount float64 `json:"amount" binding:"required" example:"100.00"`
}

// ListPendingSettlements godoc
// @Summary      待审批结账列表
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/settlements/pending [get]
func (h *AdminHandler) ListPendingSettlements(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListPendingSettlements(page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ListAllSettlements godoc
// @Summary      全部结账列表（支持 status 筛选）
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        status     query  string  false  "pending/approved/rejected，空=全部"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页条数"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/settlements [get]
func (h *AdminHandler) ListAllSettlements(c *gin.Context) {
	page, pageSize := parsePage(c)
	list, total, err := h.EarningSvc.AdminListSettlements(c.Query("status"), page, pageSize)
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// CountPendingSettlements godoc
// @Summary      待审批结账总数（角标用）
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=object}
// @Router       /admin/settlements/pending/count [get]
func (h *AdminHandler) CountPendingSettlements(c *gin.Context) {
	n, err := h.EarningSvc.AdminCountPendingSettlements()
	if err != nil {
		response.InternalError(c, "查询失败")
		return
	}
	response.OK(c, gin.H{"count": n})
}

// ReviewSettlement godoc
// @Summary      审批结账申请
// @Tags         管理端-骑手
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                       true  "结账单 ID"
// @Param        body  body  ReviewSettlementRequest   true  "审批信息"
// @Success      200  {object}  response.Body{data=model.RiderSettlement}
// @Router       /admin/settlements/{id}/review [patch]
func (h *AdminHandler) ReviewSettlement(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	operatorID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req ReviewSettlementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	st, err := h.EarningSvc.AdminReviewSettlement(id, req.Approve, operatorID, req.RejectReason)
	if err != nil {
		if errors.Is(err, service.ErrSettlementNotFound) {
			response.Fail(c, 404, 404, "结账单不存在")
			return
		}
		if errors.Is(err, service.ErrSettlementInvalid) {
			response.BadRequest(c, "结账单状态已变更")
			return
		}
		if errors.Is(err, service.ErrInsufficientEarnings) {
			response.BadRequest(c, "待结账收益不足，无法通过审批")
			return
		}
		response.InternalError(c, "审批失败")
		return
	}
	response.OK(c, st)
}

type ReviewSettlementRequest struct {
	Approve      bool    `json:"approve"`
	RejectReason *string `json:"reject_reason"`
}
