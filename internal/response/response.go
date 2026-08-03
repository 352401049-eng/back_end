package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

func Fail(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Body{Code: code, Message: message})
}

// FailWithData 业务失败且需附带结构化字段（如待付款拦截的 order_id）。
func FailWithData(c *gin.Context, httpStatus int, code int, message string, data interface{}) {
	c.JSON(httpStatus, Body{Code: code, Message: message, Data: data})
}

func BadRequest(c *gin.Context, message string) {
	Fail(c, http.StatusBadRequest, 400, message)
}

func BadRequestWithData(c *gin.Context, message string, data interface{}) {
	FailWithData(c, http.StatusBadRequest, 400, message, data)
}

func InternalError(c *gin.Context, message string) {
	Fail(c, http.StatusInternalServerError, 500, message)
}
