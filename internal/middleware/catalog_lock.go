package middleware

import (
	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/response"

	"github.com/gin-gonic/gin"
)

// RejectMerchantCatalogWrites 禁止商家账号写入商品/分类/活动；管理员可继续调用（兼容旧 /merchant 写路径）。
func RejectMerchantCatalogWrites() gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth.IsAdmin(c) {
			c.Next()
			return
		}
		t, ok := auth.AccountType(c)
		if ok && t == model.AccountTypeMerchant {
			response.Fail(c, 403, 403, "商品、分类与活动请由管理端维护")
			c.Abort()
			return
		}
		c.Next()
	}
}
