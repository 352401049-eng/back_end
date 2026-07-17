package handler

import (
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/service"
)

// HealthData 健康检查响应数据。
type HealthData struct {
	Status   string `json:"status" example:"up"`
	Database string `json:"database" example:"connected"`
}

// WeChatPhoneRequest 微信手机号授权请求。
type WeChatPhoneRequest struct {
	Code string `json:"code" binding:"required" example:"phone_code_from_getPhoneNumber"`
}

// AvatarRequest 头像授权请求。
type AvatarRequest struct {
	AvatarURL string `json:"avatar_url" binding:"required" example:"https://weixin.catmicloud.cn/uploads/2026/07/01/avatar.jpg"`
}

// MerchantProfileResp Swagger 商家资料响应。
type MerchantProfileResp = model.MerchantProfile

// ProductStoreView Swagger 用户端商品（含购买方式）。
type ProductStoreView = service.ProductStoreView

// ProductResp Swagger 商品响应。
type ProductResp = model.Product
