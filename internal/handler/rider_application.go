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

type ApplyRiderRequest struct {
	RealName string  `json:"real_name" binding:"required" example:"张三"`
	IDCardNo *string `json:"id_card_no" example:"410000199001011234"`
	Phone    string  `json:"phone" binding:"required" example:"13800138000"`
}

type ReviewRiderRequest struct {
	Status       uint8   `json:"status" binding:"required" example:"1"`
	RejectReason *string `json:"reject_reason" example:"资料不完整"`
}

// ApplyRider godoc
// @Summary      申请成为骑手
// @Description  提交骑手资格申请，待管理员审核；普通用户、商家账号可申请，管理员/骑手不可
// @Tags         用户-骑手
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ApplyRiderRequest  true  "申请信息"
// @Success      200   {object}  response.Body{data=service.RiderApplicationView}
// @Failure      400   {object}  response.Body
// @Failure      409   {object}  response.Body
// @Router       /user/rider/application [post]
func (h *UserHandler) ApplyRider(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req ApplyRiderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	app, err := h.RiderSvc.Apply(accountID, service.ApplyRiderInput{
		RealName: req.RealName, IDCardNo: req.IDCardNo, Phone: req.Phone,
	})
	if err != nil {
		handleRiderApplyError(c, err)
		return
	}
	response.OK(c, app)
}

// GetRiderApplication godoc
// @Summary      我的骑手申请
// @Description  查看最近一次骑手申请状态
// @Tags         用户-骑手
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=service.RiderApplicationView}
// @Failure      404  {object}  response.Body
// @Router       /user/rider/application [get]
func (h *UserHandler) GetRiderApplication(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	app, err := h.RiderSvc.GetLatestByAccount(accountID)
	if err != nil {
		handleRiderApplyError(c, err)
		return
	}
	response.OK(c, app)
}

// ListRiderApplications godoc
// @Summary      骑手申请列表
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页条数"
// @Param        status     query  int  false  "0=待审核 1=通过 2=拒绝"
// @Success      200  {object}  response.Body{data=query.PageResult}
// @Router       /admin/rider/applications [get]
func (h *AdminHandler) ListRiderApplications(c *gin.Context) {
	page, pageSize := parsePage(c)
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
	list, total, err := h.RiderSvc.List(page, pageSize, status)
	if err != nil {
		response.InternalError(c, "获取申请列表失败")
		return
	}
	response.OK(c, query.PageResult{List: list, Total: total, Page: page, PageSize: pageSize})
}

// GetRiderApplication godoc
// @Summary      骑手申请详情
// @Tags         管理端-骑手
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "申请 ID"
// @Success      200  {object}  response.Body{data=service.RiderApplicationView}
// @Failure      404  {object}  response.Body
// @Router       /admin/rider/applications/{id} [get]
func (h *AdminHandler) GetRiderApplication(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	app, err := h.RiderSvc.GetByID(id)
	if err != nil {
		handleRiderAdminError(c, err)
		return
	}
	response.OK(c, app)
}

// ReviewRiderApplication godoc
// @Summary      审核骑手申请
// @Description  status=1 通过（账号 is_rider=1，角色 type 不变），status=2 拒绝（需填写 reject_reason）
// @Tags         管理端-骑手
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                 true  "申请 ID"
// @Param        body  body  ReviewRiderRequest  true  "审核结果"
// @Success      200   {object}  response.Body{data=service.RiderApplicationView}
// @Failure      400   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Failure      409   {object}  response.Body
// @Router       /admin/rider/applications/{id}/review [patch]
func (h *AdminHandler) ReviewRiderApplication(c *gin.Context) {
	adminID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "ID 无效")
		return
	}
	var req ReviewRiderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	app, err := h.RiderSvc.Review(id, adminID, service.ReviewRiderInput{
		Status: req.Status, RejectReason: req.RejectReason,
	})
	if err != nil {
		handleRiderAdminError(c, err)
		return
	}
	response.OK(c, app)
}

func handleRiderApplyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRiderApplicationPending):
		response.Fail(c, 409, 409, "已有待审核的申请")
	case errors.Is(err, service.ErrAlreadyRider):
		response.Fail(c, 409, 409, "您已是骑手")
	case errors.Is(err, service.ErrRiderApplyForbidden):
		response.Fail(c, 403, 403, "当前账号类型无法申请骑手，请使用普通用户账号登录")
	case errors.Is(err, service.ErrRiderApplicationNotFound):
		response.Fail(c, 404, 404, "暂无申请记录")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, "参数无效")
	default:
		response.InternalError(c, "操作失败")
	}
}

func handleRiderAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRiderApplicationNotFound):
		response.Fail(c, 404, 404, "申请不存在")
	case errors.Is(err, service.ErrApplicationNotPending):
		response.Fail(c, 409, 409, "该申请已审核")
	case errors.Is(err, service.ErrAlreadyRider):
		response.Fail(c, 409, 409, "该用户已是骑手")
	case errors.Is(err, service.ErrInvalidReviewStatus), errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, "参数无效")
	default:
		response.InternalError(c, "操作失败")
	}
}
