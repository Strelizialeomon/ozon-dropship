// Package utils 提供统一响应壳与辅助函数（标准档：handler 不直接 c.JSON）。
//
// 响应壳 {code,message,error,data}：code=0 成功、非 0 失败；业务错误一律 HTTP 200，
// 只有 401（未登录）/403（角色不够）/429（限流）/5xx（真崩了）用非 200 状态码。
package utils

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// 业务错误码。
const (
	CodeOK           = 0    // 成功
	CodeFail         = 1    // 通用失败
	CodeValidate     = 1001 // 参数校验失败
	CodeConflict     = 1002 // 唯一键冲突（同名店、重复凭据）
	CodeNotFound     = 1004 // 目标不存在 / 已被软删
	CodeNotAdmin     = 1010 // 非管理员访问管理端点
	CodeBadCred      = 2001 // 用户名或密码错误（不透露是哪个错）
	CodeVaultUnready = 2002 // 保险箱未启用，拒绝读写凭据（绝不落明文）
)

// Response 统一响应体。
type Response struct {
	Code    int         `json:"code"`            // 0=成功，非 0=失败
	Message string      `json:"message"`         // 提示信息
	Error   string      `json:"error,omitempty"` // 错误详情（仅失败时）
	Data    interface{} `json:"data,omitempty"`  // 业务数据
}

// ResponseSuccess 构造成功响应。
func ResponseSuccess(msg string, data interface{}) Response {
	return Response{Code: CodeOK, Message: msg, Data: data}
}

// ResponseError 构造失败响应。
func ResponseError(code int, msg string, err error, data interface{}) Response {
	r := Response{Code: code, Message: msg, Data: data}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// SuccessResp 成功（HTTP 200，code=0）。
func SuccessResp(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusOK, ResponseSuccess(msg, data))
}

// FailResp 通用业务失败（HTTP 200，code=1）。
func FailResp(c *gin.Context, msg string, err error) {
	c.JSON(http.StatusOK, ResponseError(CodeFail, msg, err, nil))
}

// FailWithCode 指定业务错误码的失败（HTTP 200）。
func FailWithCode(c *gin.Context, code int, msg string, err error, data interface{}) {
	c.JSON(http.StatusOK, ResponseError(code, msg, err, data))
}

// ValidateError 参数校验失败（HTTP 200，code=1001）。
func ValidateError(c *gin.Context, err error) {
	var errs validator.ValidationErrors
	msg := "参数校验失败"
	if errors.As(err, &errs) && len(errs) > 0 {
		e := errs[0]
		msg = fmt.Sprintf("字段 %s 校验失败（规则: %s）", e.Field(), e.Tag())
	}
	c.JSON(http.StatusOK, ResponseError(CodeValidate, msg, err, nil))
}

// Unauthorized 未登录 / 会话失效（HTTP 401）。
func Unauthorized(c *gin.Context, msg string) {
	c.JSON(http.StatusUnauthorized, ResponseError(CodeFail, msg, errors.New("未授权"), nil))
}

// Forbidden 已登录但角色不够（HTTP 403）。
func Forbidden(c *gin.Context, msg string) {
	c.JSON(http.StatusForbidden, ResponseError(CodeNotAdmin, msg, errors.New("权限不足"), nil))
}

// TooManyRequests 限流（HTTP 429）。
func TooManyRequests(c *gin.Context, msg string) {
	c.JSON(http.StatusTooManyRequests, ResponseError(CodeFail, msg, errors.New("请求过于频繁"), nil))
}

// ServerError 服务器内部错误（HTTP 500）；err 细节只进日志，不回给客户端。
func ServerError(c *gin.Context, msg string, err error) {
	if err != nil {
		logger.Errorf("[5xx] %s: %v", msg, err)
	}
	c.JSON(http.StatusInternalServerError, ResponseError(CodeFail, msg, errors.New("服务器内部错误"), nil))
}
