package handler

import (
	"errors"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"
	"yujixinjiang/backend/internal/wechat"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct {
	Svc *service.AuthService
}

type LoginRequest struct {
	Code string `json:"code" binding:"required" example:"081abcXYZ"`
}

type LoginResponse struct {
	Token   string        `json:"token"`
	Account model.Account `json:"account"`
	IsNew   bool          `json:"is_new"`
}

// Login godoc
// @Summary      微信登录
// @Description  统一登录入口：wx.login 取得 code 后换取 JWT。已绑定 openid 的商家/管理员/骑手按原角色登录，否则自动注册为普通用户
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "wx.login 返回的 code"
// @Success      200   {object}  response.Body{data=LoginResponse}
// @Failure      400   {object}  response.Body
// @Failure      403   {object}  response.Body
// @Failure      503   {object}  response.Body
// @Router       /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请提供 code 字段")
		return
	}

	result, err := h.Svc.LoginByWeChatCode(req.Code)
	if err != nil {
		h.handleLoginError(c, err)
		return
	}

	response.OK(c, LoginResponse{
		Token:   result.Token,
		Account: result.Account,
		IsNew:   result.IsNew,
	})
}

// Me godoc
// @Summary      当前登录用户
// @Description  返回 JWT 对应账号信息
// @Tags         认证
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=model.Account}
// @Failure      401  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}

	account, err := h.Svc.GetAccount(accountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, 404, 404, "用户不存在")
			return
		}
		response.InternalError(c, "查询用户失败")
		return
	}

	response.OK(c, account)
}

// WeChatPhone godoc
// @Summary      绑定微信手机号
// @Description  使用 getPhoneNumber 组件返回的 code 换取并绑定手机号
// @Tags         认证
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      WeChatPhoneRequest  true  "手机号授权 code"
// @Success      200   {object}  response.Body{data=model.Account}
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Failure      409   {object}  response.Body
// @Failure      503   {object}  response.Body
// @Router       /auth/wechat/phone [post]
func (h *AuthHandler) WeChatPhone(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}

	var req WeChatPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请提供 code 字段")
		return
	}

	account, err := h.Svc.BindWeChatPhone(accountID, req.Code)
	if err != nil {
		h.handleProfileError(c, err)
		return
	}

	response.OK(c, account)
}

// Avatar godoc
// @Summary      更新用户头像
// @Description  保存 chooseAvatar 选择的头像地址
// @Tags         认证
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      AvatarRequest  true  "头像地址"
// @Success      200   {object}  response.Body{data=model.Account}
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Router       /auth/avatar [post]
func (h *AuthHandler) Avatar(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}

	var req AvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请提供 avatar_url 字段")
		return
	}

	account, err := h.Svc.UpdateAvatar(accountID, req.AvatarURL)
	if err != nil {
		response.InternalError(c, "更新头像失败")
		return
	}

	response.OK(c, account)
}

// UpdateProfile 更新昵称等资料
type UpdateProfileRequest struct {
	Nickname *string `json:"nickname" example:"新昵称"`
}

// UpdateProfile godoc
// @Summary      更新用户资料
// @Tags         认证
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  UpdateProfileRequest  true  "资料"
// @Success      200   {object}  response.Body{data=model.Account}
// @Router       /auth/profile [patch]
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	if req.Nickname == nil || *req.Nickname == "" {
		response.BadRequest(c, "请提供 nickname")
		return
	}
	account, err := h.Svc.UpdateNickname(accountID, *req.Nickname)
	if err != nil {
		response.InternalError(c, "更新失败")
		return
	}
	response.OK(c, account)
}

func (h *AuthHandler) handleLoginError(c *gin.Context, err error) {
	var apiErr *wechat.APIError
	switch {
	case errors.As(err, &apiErr):
		response.Fail(c, 400, 400, wechat.UserMessage(apiErr.ErrCode))
	case errors.Is(err, service.ErrWeChatNotConfigured):
		response.Fail(c, 503, 503, "请先在 .env 中配置 WECHAT_APPID 和 WECHAT_SECRET")
	case errors.Is(err, service.ErrAccountDisabled):
		response.Fail(c, 403, 403, "账号已被禁用")
	default:
		response.InternalError(c, "登录失败")
	}
}

func (h *AuthHandler) handleProfileError(c *gin.Context, err error) {
	var apiErr *wechat.APIError
	switch {
	case errors.As(err, &apiErr):
		response.Fail(c, 400, 400, wechat.UserMessage(apiErr.ErrCode))
	case errors.Is(err, service.ErrWeChatNotConfigured):
		response.Fail(c, 503, 503, "请先在 .env 中配置 WECHAT_APPID 和 WECHAT_SECRET")
	case errors.Is(err, service.ErrPhoneAlreadyBound):
		response.Fail(c, 409, 409, "该手机号已被其他账号绑定")
	default:
		response.InternalError(c, "绑定手机号失败")
	}
}
