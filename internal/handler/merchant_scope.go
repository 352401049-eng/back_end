package handler

import (
	"errors"
	"strconv"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"
	"yujixinjiang/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// resolveMerchantScope 商家账号取自身店铺；管理员需传 merchant_id（query 或 Header X-Merchant-Id）。
func resolveMerchantScope(c *gin.Context, merchantSvc *service.MerchantService) (*uint64, error) {
	accountType, ok := auth.AccountType(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return nil, errors.New("unauthorized")
	}

	if accountType == model.AccountTypeAdmin {
		raw := c.Query("merchant_id")
		if raw == "" {
			raw = c.GetHeader("X-Merchant-Id")
		}
		if raw == "" {
			response.BadRequest(c, "管理员进入商家端请指定 merchant_id")
			return nil, errors.New("merchant_id required")
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "merchant_id 无效")
			return nil, err
		}
		if _, err := merchantSvc.GetByID(id); err != nil {
			if errors.Is(err, service.ErrMerchantNotFound) {
				response.Fail(c, 404, 404, "商家不存在")
				return nil, err
			}
			response.InternalError(c, "获取商家失败")
			return nil, err
		}
		return &id, nil
	}

	accountID, ok := auth.AccountID(c)
	if !ok {
		response.Fail(c, 401, 401, "未登录")
		return nil, errors.New("unauthorized")
	}
	profile, err := merchantSvc.GetByAccountID(accountID)
	if err != nil {
		if errors.Is(err, service.ErrMerchantNotFound) {
			response.Fail(c, 404, 404, "商家资料不存在")
			return nil, err
		}
		response.InternalError(c, "获取商家资料失败")
		return nil, err
	}
	id := profile.ID
	return &id, nil
}

// resolveMerchantProfile 返回商家资料（含管理员代管场景）。
func resolveMerchantProfile(c *gin.Context, merchantSvc *service.MerchantService) (*model.MerchantProfile, error) {
	id, err := resolveMerchantScope(c, merchantSvc)
	if err != nil {
		return nil, err
	}
	return merchantSvc.GetByID(*id)
}
