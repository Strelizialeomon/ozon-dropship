package alibaba

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// APIError 1688 网关/业务层失败：HTTP 通了但请求被拒（业务拒绝、签名错、超限等）。
// 与网络失败区分开：网络失败可重试，这里把「业务拒绝」经 ratelimit.Permanent 包装后直接上抛、
// 不重试；「超限」按 ratelimit.RateLimitedError 走退避重试（见 ratelimit.go）。
type APIError struct {
	// API 接口全名，如 alibaba.trade.fastCreateOrder。
	API string
	// Code 网关错误码（如 500_004、404），可能为空。
	Code string
	// Message 网关错误文案。
	Message string
	// HTTP 非 0 表示是 HTTP 状态码层面的失败（4xx/5xx），Code 里放状态码。
	HTTP int
	// Body 截断后的响应体，排障用（请求侧签名不在响应里，无泄漏风险）。
	Body string
}

func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.Code != "":
		return fmt.Sprintf("1688 %s: %s: %s", e.API, e.Code, e.Message)
	case e.Code != "":
		return fmt.Sprintf("1688 %s: %s", e.API, e.Code)
	case e.Message != "":
		return fmt.Sprintf("1688 %s: %s", e.API, e.Message)
	default:
		return "1688 " + e.API + ": 请求失败"
	}
}

// Code 返回 err 携带的 1688 错误码（非 APIError 返回空串）。
func Code(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// IsCode 判断 err 是否带指定错误码之一（大小写不敏感）。
func IsCode(err error, codes ...string) bool {
	got := Code(err)
	if got == "" {
		return false
	}
	for _, c := range codes {
		if strings.EqualFold(got, c) {
			return true
		}
	}
	return false
}

// flexBool 兼容 1688 的三种布尔写法：true / "true" / 缺省。
// 有的接口把 success 标成 String 类型（如 alibaba.trade.get.buyerView 返回 "true"）。
type flexBool struct {
	Value bool
	Valid bool // 字段是否存在
}

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	switch strings.ToLower(s) {
	case "":
		return nil
	case "null":
		return nil
	case "true", "1":
		b.Value, b.Valid = true, true
	case "false", "0":
		b.Value, b.Valid = false, true
	}
	return nil
}

// FlexString 兼容「文档标 String、实际返回数字」的 id 字段（如 idOfStr、subItemIDString）。
// 订单号一类的大整数 id 也统一用它：数字原样保留文本，不经 float64、不受 int64 上限约束。
type FlexString string

func (s *FlexString) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*s = ""
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*s = FlexString(v)
		return nil
	}
	*s = FlexString(raw) // 数字原样保留（id 可能超出 float64 精度，不许经手浮点）
	return nil
}

// String 取字符串值。
func (s FlexString) String() string { return string(s) }

// FlexTime 1688 的时间字段格式不统一，见过这些：
//   - "20170913231916000-0700"（yyyyMMddHHmmssSSS±ZZZZ，订单时间）
//   - "2018-07-24 21:55:33"（物流轨迹）
//   - "2018-02-07 15:27:58"、"2026-10-01"（个别字段）
//
// 解析成功填 Time；解析失败不让整个响应报废——保留原文到 Raw，Time 为零值。
type FlexTime struct {
	time.Time
	// Raw 原始文本（无论解析成功与否都记，排障用）。
	Raw string
}

// flexTimeLayouts 依次尝试的布局。毫秒连在秒后面、没有小数点的写法用 "000" 表示；
// refresh_token_timeout 是 14 位（yyyyMMddHHmmss±ZZZZ），单独一条。
var flexTimeLayouts = []string{
	"20060102150405000-0700",
	"20060102150405-0700",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02",
}

func (t *FlexTime) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		return nil
	}
	t.Raw = raw
	for _, layout := range flexTimeLayouts {
		if v, err := time.Parse(layout, raw); err == nil {
			t.Time = v
			return nil
		}
	}
	return nil
}

// Parsed 是否解析成功。
func (t FlexTime) Parsed() bool { return !t.IsZero() }

// statusFields 各接口的成功/错误字段写法不一，统一收进这一个结构体（大小写与下划线变体都收）：
//   - 订单详情：success 是字符串 "true"；
//   - 下单/预览：errorMsg，下单失败另见 littlebossERP 生产代码用的 error_message；
//   - 多数接口：errorCode + errorMessage。
//
// success 缺省时不当作失败（官方多个出参示例整段省略 success；订单列表接口根本不返回它），
// 只有显式 false 或出现错误码才算失败。
type statusFields struct {
	Success           flexBool `json:"success"`
	ErrorCode         string   `json:"errorCode"`
	ErrorCodeSnake    string   `json:"error_code"`
	ErrorMessage      string   `json:"errorMessage"`
	ErrorMessageSnake string   `json:"error_message"`
	ErrorMsg          string   `json:"errorMsg"`
	Error             string   `json:"error"` // getToken 的 OAuth 风格错误码（未验）
	ErrorDesc         string   `json:"error_description"`
	Message           string   `json:"message"`
	Code              string   `json:"code"`
}

// code 首个非空的错误码。
func (s statusFields) code() string {
	for _, c := range []string{s.ErrorCode, s.ErrorCodeSnake, s.Code, s.Error} {
		if c != "" {
			return c
		}
	}
	return ""
}

// message 首个非空的错误文案。
func (s statusFields) message() string {
	for _, m := range []string{s.ErrorMessage, s.ErrorMessageSnake, s.ErrorMsg, s.Message, s.ErrorDesc} {
		if m != "" {
			return m
		}
	}
	return ""
}

// okCodes 成功码口径：code/errorCode 等于这些值（大小写不敏感）不算错误。
// 来源：社区实现（Natawat-d/1688_Platform 的 okCode）对真实网关的归纳；【未验】。
// 不设这层，`{"success":true,"code":"0","message":"成功"}` 这类成功信封会被误判成失败。
var okCodes = map[string]bool{
	"": true, "0": true, "200": true, "s0000": true, "success": true,
}

func okCode(code string) bool {
	return okCodes[strings.ToLower(code)]
}

// err 按成功/失败字段判断；失败返回 *APIError，成功返回 nil。
// hasResult=false（连结果字段都没有时）由各调用点单独判断，不在这里兜。
func (s statusFields) err(apiName string) *APIError {
	code := s.code()
	if s.Success.Valid && !s.Success.Value {
		return &APIError{API: apiName, Code: firstNonEmpty(code, "FAILED"), Message: s.message()}
	}
	if code != "" && !okCode(code) {
		return &APIError{API: apiName, Code: code, Message: s.message()}
	}
	// 没有 success 字段、没有（有效的）错误码、但有错误文案：按失败处理
	// （订单列表这类接口的失败形态）。注意排除 code 为成功码（"0"/"200"）的响应。
	if !s.Success.Valid && code == "" && s.message() != "" {
		return &APIError{API: apiName, Code: "FAILED", Message: s.message()}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// truncateBody 截断响应体，排障日志最多留 2KB。
func truncateBody(b []byte) string {
	const max = 2000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
