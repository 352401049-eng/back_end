package auth

import (
	"yujixinjiang/backend/internal/model"

	"github.com/gin-gonic/gin"
)

const (
	ContextAccountID   = "account_id"
	ContextAccountType = "account_type"
	ContextIsRider     = "is_rider"
)

func AccountID(c *gin.Context) (uint64, bool) {
	v, ok := c.Get(ContextAccountID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint64)
	return id, ok
}

func AccountType(c *gin.Context) (uint8, bool) {
	v, ok := c.Get(ContextAccountType)
	if !ok {
		return 0, false
	}
	t, ok := v.(uint8)
	return t, ok
}

func IsRider(c *gin.Context) bool {
	v, ok := c.Get(ContextIsRider)
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func IsAdmin(c *gin.Context) bool {
	t, ok := AccountType(c)
	return ok && t == model.AccountTypeAdmin
}

func MustAccountID(c *gin.Context) uint64 {
	id, ok := AccountID(c)
	if !ok {
		return 0
	}
	return id
}
