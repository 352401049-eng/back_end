package middleware

import (
	"strings"

	"yujixinjiang/backend/internal/auth"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// RequireAuth 校验 JWT，并确认账号存在且处于正常状态。
// 除登录、健康检查等公开接口外，所有信息类接口均应挂载此中间件。
func RequireAuth(jwtSecret string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Fail(c, 401, 401, "请先登录")
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			response.Fail(c, 401, 401, "登录已过期，请重新登录")
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			response.Fail(c, 401, 401, "无效的 token")
			c.Abort()
			return
		}

		accountID, ok := claims["account_id"].(float64)
		if !ok || accountID <= 0 {
			response.Fail(c, 401, 401, "无效的 token")
			c.Abort()
			return
		}

		accountType, ok := claims["account_type"].(float64)
		if !ok {
			response.Fail(c, 401, 401, "无效的 token")
			c.Abort()
			return
		}

		var account model.Account
		if err := query.NotDeleted(db).Select("id", "type", "status", "is_rider").First(&account, uint64(accountID)).Error; err != nil {
			response.Fail(c, 401, 401, "账号不存在或已失效")
			c.Abort()
			return
		}

		if account.Status != 1 {
			response.Fail(c, 403, 403, "账号已被禁用")
			c.Abort()
			return
		}

		if account.Type != uint8(accountType) {
			response.Fail(c, 401, 401, "登录信息已变更，请重新登录")
			c.Abort()
			return
		}

		c.Set(auth.ContextAccountID, account.ID)
		c.Set(auth.ContextAccountType, account.Type)
		c.Set(auth.ContextIsRider, account.IsRider == 1)
		c.Next()
	}
}

// OptionalAuth 若携带有效 JWT 则写入账号上下文，未登录或 token 无效时不拦截。
func OptionalAuth(jwtSecret string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			c.Next()
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			c.Next()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.Next()
			return
		}

		accountID, ok := claims["account_id"].(float64)
		if !ok || accountID <= 0 {
			c.Next()
			return
		}

		accountType, ok := claims["account_type"].(float64)
		if !ok {
			c.Next()
			return
		}

		var account model.Account
		if err := query.NotDeleted(db).Select("id", "type", "status", "is_rider").First(&account, uint64(accountID)).Error; err != nil {
			c.Next()
			return
		}
		if account.Status != 1 || account.Type != uint8(accountType) {
			c.Next()
			return
		}

		c.Set(auth.ContextAccountID, account.ID)
		c.Set(auth.ContextAccountType, account.Type)
		c.Set(auth.ContextIsRider, account.IsRider == 1)
		c.Next()
	}
}

// RequireRider 限制仅已通过骑手审核的账号可访问；管理员可 bypass 用于联调测试。
func RequireRider() gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth.IsAdmin(c) {
			c.Next()
			return
		}
		if !auth.IsRider(c) {
			response.Fail(c, 403, 403, "您还不是骑手或未通过审核")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAccountTypes 限制仅指定角色可访问（需在 RequireAuth 之后使用）；管理员可 bypass。
func RequireAccountTypes(types ...uint8) gin.HandlerFunc {
	allowed := make(map[uint8]struct{}, len(types))
	for _, t := range types {
		allowed[t] = struct{}{}
	}

	return func(c *gin.Context) {
		if auth.IsAdmin(c) {
			c.Next()
			return
		}

		accountType, ok := auth.AccountType(c)
		if !ok {
			response.Fail(c, 401, 401, "请先登录")
			c.Abort()
			return
		}

		if _, ok := allowed[accountType]; !ok {
			response.Fail(c, 403, 403, "无权访问")
			c.Abort()
			return
		}

		c.Next()
	}
}
