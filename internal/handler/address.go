package handler

import (
	"errors"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type AddressRequest struct {
	ContactName  string              `json:"contact_name" binding:"required" example:"张三"`
	ContactPhone string              `json:"contact_phone" binding:"required" example:"13800138000"`
	Province     string              `json:"province" binding:"required" example:"河南省"`
	City         string              `json:"city" binding:"required" example:"信阳市"`
	District     string              `json:"district" binding:"required" example:"浉河区"`
	Detail       string              `json:"detail" binding:"required" example:"某某路 1 号"`
	IsDefault    uint8               `json:"is_default" example:"0"`
	Latitude     FlexNullableFloat64 `json:"latitude"`
	Longitude    FlexNullableFloat64 `json:"longitude"`
	LocationName FlexNullableString  `json:"location_name"`
}

type AddressPatchRequest struct {
	ContactName  *string             `json:"contact_name"`
	ContactPhone *string             `json:"contact_phone"`
	Province     *string             `json:"province"`
	City         *string             `json:"city"`
	District     *string             `json:"district"`
	Detail       *string             `json:"detail"`
	IsDefault    *uint8              `json:"is_default"`
	Latitude     FlexNullableFloat64 `json:"latitude"`
	Longitude    FlexNullableFloat64 `json:"longitude"`
	LocationName FlexNullableString  `json:"location_name"`
}

// ListAddresses godoc
// @Summary      收货地址列表
// @Tags         用户-地址
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Body{data=[]model.UserAddress}
// @Router       /user/addresses [get]
func (h *UserHandler) ListAddresses(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	list, err := h.AddressSvc.List(accountID)
	if err != nil {
		response.InternalError(c, "获取地址列表失败")
		return
	}
	response.OK(c, list)
}

// CreateAddress godoc
// @Summary      新增收货地址
// @Tags         用户-地址
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  AddressRequest  true  "地址信息"
// @Success      200   {object}  response.Body{data=model.UserAddress}
// @Router       /user/addresses [post]
func (h *UserHandler) CreateAddress(c *gin.Context) {
	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return
	}
	var req AddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	input, err := toAddressInput(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	addr, err := h.AddressSvc.Create(accountID, input)
	if err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, addr)
}

// GetAddress godoc
// @Summary      收货地址详情
// @Tags         用户-地址
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "地址 ID"
// @Success      200  {object}  response.Body{data=model.UserAddress}
// @Router       /user/addresses/{id} [get]
func (h *UserHandler) GetAddress(c *gin.Context) {
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
	addr, err := h.AddressSvc.Get(accountID, id)
	if err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, addr)
}

// UpdateAddress godoc
// @Summary      更新收货地址
// @Tags         用户-地址
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int             true  "地址 ID"
// @Param        body  body  AddressRequest  true  "地址信息"
// @Success      200   {object}  response.Body{data=model.UserAddress}
// @Router       /user/addresses/{id} [put]
func (h *UserHandler) UpdateAddress(c *gin.Context) {
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
	var req AddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	input, err := toAddressInput(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	addr, err := h.AddressSvc.Update(accountID, id, input)
	if err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, addr)
}

// PatchAddress godoc
// @Summary      部分更新收货地址
// @Tags         用户-地址
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int                  true  "地址 ID"
// @Param        body  body  AddressPatchRequest  true  "要更新的字段"
// @Success      200   {object}  response.Body{data=model.UserAddress}
// @Router       /user/addresses/{id} [patch]
func (h *UserHandler) PatchAddress(c *gin.Context) {
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
	var req AddressPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数无效")
		return
	}
	input, err := toAddressPatchInput(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if !addressPatchHasField(input) {
		response.BadRequest(c, "请至少传一个要更新的字段")
		return
	}
	addr, err := h.AddressSvc.Patch(accountID, id, input)
	if err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, addr)
}

// DeleteAddress godoc
// @Summary      删除收货地址
// @Description  逻辑删除
// @Tags         用户-地址
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "地址 ID"
// @Success      200  {object}  response.Body
// @Router       /user/addresses/{id} [delete]
func (h *UserHandler) DeleteAddress(c *gin.Context) {
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
	if err := h.AddressSvc.Delete(accountID, id); err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, nil)
}

// SetDefaultAddress godoc
// @Summary      设为默认地址
// @Tags         用户-地址
// @Produce      json
// @Security     BearerAuth
// @Param        id   path  int  true  "地址 ID"
// @Success      200  {object}  response.Body{data=model.UserAddress}
// @Router       /user/addresses/{id}/default [patch]
func (h *UserHandler) SetDefaultAddress(c *gin.Context) {
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
	addr, err := h.AddressSvc.SetDefault(accountID, id)
	if err != nil {
		handleAddressError(c, err)
		return
	}
	response.OK(c, addr)
}

func toAddressInput(req AddressRequest) (service.AddressInput, error) {
	loc, err := parseAddressLocationFields(req.Latitude, req.Longitude)
	if err != nil {
		return service.AddressInput{}, err
	}
	input := service.AddressInput{
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		Province: req.Province, City: req.City, District: req.District,
		Detail: req.Detail, IsDefault: req.IsDefault,
		Location: loc,
	}
	if req.LocationName.Present {
		input.LocationNameSet = true
		input.LocationName = req.LocationName.Ptr()
	}
	return input, nil
}

func toAddressPatchInput(req AddressPatchRequest) (service.AddressPatchInput, error) {
	loc, err := parseAddressLocationFields(req.Latitude, req.Longitude)
	if err != nil {
		return service.AddressPatchInput{}, err
	}
	input := service.AddressPatchInput{
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		Province: req.Province, City: req.City, District: req.District,
		Detail: req.Detail, IsDefault: req.IsDefault,
		Location: loc,
	}
	if req.LocationName.Present {
		input.LocationNameSet = true
		input.LocationName = req.LocationName.Ptr()
	}
	return input, nil
}

func addressPatchHasField(in service.AddressPatchInput) bool {
	return in.ContactName != nil || in.ContactPhone != nil || in.Province != nil ||
		in.City != nil || in.District != nil || in.Detail != nil || in.IsDefault != nil ||
		in.Location != nil || in.LocationNameSet
}

func parseAddressLocationFields(lat, lng FlexNullableFloat64) (*service.AddressLocationInput, error) {
	if !lat.Present && !lng.Present {
		return nil, nil
	}
	if lat.Present != lng.Present {
		return nil, errors.New("latitude 与 longitude 需成对填写")
	}
	if lat.Null && lng.Null {
		return &service.AddressLocationInput{Clear: true}, nil
	}
	if lat.Null || lng.Null {
		return nil, errors.New("latitude 与 longitude 需成对填写")
	}
	return &service.AddressLocationInput{Update: true, Lat: lat.Value, Lng: lng.Value}, nil
}

func handleAddressError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAddressNotFound):
		response.Fail(c, 404, 404, "地址不存在")
	case errors.Is(err, service.ErrInvalidProductArg):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "操作失败")
	}
}

var _ model.UserAddress
