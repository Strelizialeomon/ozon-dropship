package ozon

import "fmt"

// APIError Ozon 返回的结构化错误（官方 rpcStatus 口径）。
//
// 出现位置：
//   - 4xx（除 429）被包进 *ratelimit.PermanentError 返回——参数错、权限不足，
//     重试无意义；
//   - 429 / 5xx 裸返回（可重试），429 的外层是 *ratelimit.RateLimitedError。
//
// 判断方式：errors.As(err, &apiErr)；解包后的具体错误码看 Code。
type APIError struct {
	// StatusCode HTTP 状态码。
	StatusCode int
	// Code Ozon 错误码（响应体 code 字段）。
	Code int32
	// Message 错误描述。
	Message string
	// Details 附加信息（protobuf Any：typeUrl + base64 value）。
	Details []ErrorDetail
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Ozon API 错误: HTTP %d, code=%d, message=%q", e.StatusCode, e.Code, e.Message)
}

// ErrorDetail 错误附加信息（官方 rpcStatus.details 的元素格式）。
type ErrorDetail struct {
	TypeURL string `json:"typeUrl"`
	Value   string `json:"value"`
}
