package handler

import (
	"yujixinjiang/backend/internal/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type HealthHandler struct {
	DB *gorm.DB
}

// Check godoc
// @Summary      健康检查
// @Description  检查服务与数据库连接状态
// @Tags         系统
// @Produce      json
// @Success      200  {object}  response.Body{data=HealthData}
// @Failure      500  {object}  response.Body
// @Router       /health [get]
func (h *HealthHandler) Check(c *gin.Context) {
	sqlDB, err := h.DB.DB()
	if err != nil {
		response.InternalError(c, "数据库状态异常")
		return
	}
	if err := sqlDB.Ping(); err != nil {
		response.InternalError(c, "数据库 ping 失败")
		return
	}

	response.OK(c, HealthData{
		Status:   "up",
		Database: "connected",
	})
}
