package wechatv3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError 微信 V3 API 返回的业务错误。
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("wechat v3 err: status=%d code=%s message=%s", e.StatusCode, e.Code, e.Message)
	if e.Detail != "" {
		msg += " detail=" + e.Detail
	}
	return msg
}

// parseAPIError 解析微信 V3 的错误响应体。
func parseAPIError(statusCode int, body []byte) error {
	if statusCode == http.StatusOK || statusCode == http.StatusNoContent {
		return nil
	}
	apiErr := &APIError{StatusCode: statusCode}
	if err := json.Unmarshal(body, apiErr); err != nil {
		apiErr.Code = "HTTP_ERROR"
		apiErr.Message = fmt.Sprintf("HTTP %d: %s", statusCode, string(body))
	}
	return apiErr
}

// isNotFound 判断微信返回是否为订单不存在。
func isNotFound(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusNotFound ||
			apiErr.Code == "ORDER_NOT_FOUND" || apiErr.Code == "RESOURCE_NOT_EXISTS"
	}
	return false
}

// IsOrderPaid 判断关闭订单时微信是否表示「该单已支付」。
func IsOrderPaid(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		code := strings.ToUpper(apiErr.Code)
		return code == "ORDERPAID" || code == "ORDER_PAID"
	}
	return false
}
