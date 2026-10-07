// Package ozon 是 Ozon Seller API 客户端（S1-B，总纲 §7.1）。
//
// 每店一个实例：Client-Id + Api-Key 由装配层从凭据保险箱取出后传入
// （本包不管凭据存取）。所有请求经 infra/ratelimit 的 Registry.Do：
// 先按「店铺总闸 + 单接口桶」排队，429 读 Retry-After、其余失败指数退避，
// 次数用尽返回 *ratelimit.ExhaustedError（总纲 §5.5）。
//
// 接口版本以官方现行文档为准（2026-10 核对 docs.ozon.ru swagger）：
// /v4/posting/fbs/list、/v4/posting/fbs/unfulfilled/list、/v3/posting/fbs/get、
// /v4/posting/fbs/ship、/v2/posting/fbs/package-label、
// /v2/fbs/posting/tracking-number/set、/v1/roles、/v2/delivery-method/list。
package ozon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// DefaultBaseURL Ozon Seller API 基址（官方文档 servers：api-seller.ozon.ru）。
const DefaultBaseURL = "https://api-seller.ozon.ru"

// userAgent 便于平台侧辨识调用方。
const userAgent = "ozon-dropship-backend (S1-B)"

// maxBodyBytes 响应体读取上限：面单 PDF 可到几 MB，正常 JSON 远小于此。
const maxBodyBytes = 32 << 20

// defaultHTTPTimeout 单次 HTTP 请求超时（重试由限流器管，这里只防挂死）。
const defaultHTTPTimeout = 30 * time.Second

// 接口路径（版本号照官方现行版）。
const (
	pathPostingList        = "/v4/posting/fbs/list"
	pathPostingUnfulfilled = "/v4/posting/fbs/unfulfilled/list"
	pathPostingGet         = "/v3/posting/fbs/get"
	pathPostingShip        = "/v4/posting/fbs/ship"
	pathPostingLabel       = "/v2/posting/fbs/package-label"
	pathLabelTaskCreate    = "/v3/posting/fbs/package-label/create"
	pathLabelTaskGet       = "/v2/posting/fbs/package-label/get"
	pathTrackingNumberSet  = "/v2/fbs/posting/tracking-number/set"
	pathRoles              = "/v1/roles"
	pathDeliveryMethodList = "/v2/delivery-method/list"
)

// 限流桶的接口名（配置 ratelimit.ozon.endpoints 的键；只有配了限额的才受单接口桶）。
const (
	EndpointPostingList        = "posting/fbs/list"
	EndpointPostingUnfulfilled = "posting/fbs/unfulfilled/list"
	EndpointPostingGet         = "posting/fbs/get"
	EndpointPostingShip        = "posting/fbs/ship"
	EndpointPostingLabel       = "posting/fbs/package-label"
	EndpointLabelTaskCreate    = "posting/fbs/package-label/create"
	EndpointLabelTaskGet       = "posting/fbs/package-label/get"
	EndpointTrackingNumberSet  = "fbs/posting/tracking-number/set"
	EndpointRoles              = "roles"
	EndpointDeliveryMethodList = "delivery-method/list"
)

// Options 客户端构造参数。
type Options struct {
	// ClientID Ozon 卖家 Client-Id（请求头）。
	ClientID string
	// APIKey Ozon Api-Key（请求头；由调用方从保险箱解密后传入）。
	APIKey string
	// Subject 限流桶主体：官方限额按 Client-Id 计（每 Client-Id 50 次/秒），
	// 传 Client-Id；留空同样退化为 ClientID（两店共用同一 Client-Id 时共桶，防绕过总闸）。
	Subject string
	// BaseURL 覆盖 API 基址（测试 / 代理用）；空 = DefaultBaseURL。
	BaseURL string
	// HTTP 覆盖 HTTP 客户端；空 = 30 秒超时的默认客户端。
	HTTP *http.Client
}

// Client Ozon 客户端（单店一个实例）。
type Client struct {
	baseURL  string
	clientID string
	apiKey   string
	subject  string
	http     *http.Client
	limiter  *ratelimit.Registry
}

// New 构造客户端。limiter 必填——所有出站请求都要过限流器（总纲 §5.5），
// 传 nil 会 panic（宁可起不来，也别静默不限流）。
func New(limiter *ratelimit.Registry, opts Options) *Client {
	if limiter == nil {
		panic("ozon.New: limiter 不能为 nil（所有请求必须经限流器）")
	}
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	subject := opts.Subject
	if subject == "" {
		subject = opts.ClientID
	}
	hc := opts.HTTP
	if hc == nil {
		hc = &http.Client{
			Timeout: defaultHTTPTimeout,
			// 拒绝跟随重定向：Go 的跨域重定向会保留自定义认证头（只剥 Authorization /
			// Cookie），不能让 Client-Id / Api-Key 跟着 3xx 跑到别的域。跟随被拒时
			// http.Do 返回最后一个响应，3xx 会走下面的非 200 分支。
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{
		baseURL:  strings.TrimRight(base, "/"),
		clientID: opts.ClientID,
		apiKey:   opts.APIKey,
		subject:  subject,
		http:     hc,
		limiter:  limiter,
	}
}

// do 限流 + 重试地调一个接口，并把 200 的 JSON 响应解析进 out（out 可为 nil）。
func (c *Client) do(ctx context.Context, endpoint, path string, req, out any) error {
	body, err := c.doRaw(ctx, endpoint, path, req)
	if err != nil {
		return err
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		// 响应体解析不了 = 契约不符，重试也解不出来。
		return ratelimit.Permanent(fmt.Errorf("解析 Ozon 响应失败（%s）: %w", path, err))
	}
	return nil
}

// doRaw 同 do，但返回原始响应体（面单 PDF 用）。
func (c *Client) doRaw(ctx context.Context, endpoint, path string, req any) ([]byte, error) {
	var body []byte
	err := c.limiter.Do(ctx, ratelimit.ScopeOzon, c.subject, endpoint, func(ctx context.Context) error {
		var err error
		body, err = c.call(ctx, path, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// call 执行一次 HTTP 请求并按状态码归一错误：
//   - 200 → 返回响应体；
//   - 429 → *ratelimit.RateLimitedError（带 Retry-After，缺省交给指数退避）；
//   - 5xx → 普通错误（限流器指数退避重试）；
//   - 其余 4xx → *ratelimit.PermanentError 包 *APIError（参数错、权限不足，重试无意义）。
func (c *Client) call(ctx context.Context, path string, req any) ([]byte, error) {
	var reader io.Reader
	if req != nil {
		payload, err := json.Marshal(req)
		if err != nil {
			return nil, ratelimit.Permanent(fmt.Errorf("编码 Ozon 请求失败（%s）: %w", path, err))
		}
		reader = bytes.NewReader(payload)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return nil, ratelimit.Permanent(fmt.Errorf("构造 Ozon 请求失败（%s）: %w", path, err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Client-Id", c.clientID)
	httpReq.Header.Set("Api-Key", c.apiKey)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// 网络错误（连接失败 / 超时 / 中断）：可重试。
		return nil, fmt.Errorf("请求 Ozon 失败（%s）: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 Ozon 响应失败（%s）: %w", path, err)
	}
	if len(body) > maxBodyBytes {
		// 显式报错而不是返回截断后的字节：二进制（面单 PDF）被静默截断会产出坏文件。
		return nil, ratelimit.Permanent(fmt.Errorf("响应体超过 %d 字节上限（%s）", maxBodyBytes, path))
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &ratelimit.RateLimitedError{
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Err:        apiError(resp.StatusCode, body),
		}
	case resp.StatusCode >= 500:
		return nil, apiError(resp.StatusCode, body)
	default:
		return nil, ratelimit.Permanent(apiError(resp.StatusCode, body))
	}
}

// apiError 把非 200 响应体（官方 rpcStatus：{code, message, details}）归一为 *APIError。
func apiError(status int, body []byte) *APIError {
	e := &APIError{StatusCode: status}
	var rpc struct {
		Code    int32         `json:"code"`
		Message string        `json:"message"`
		Details []ErrorDetail `json:"details"`
	}
	if err := json.Unmarshal(body, &rpc); err == nil {
		e.Code = rpc.Code
		e.Message = rpc.Message
		e.Details = rpc.Details
	}
	if e.Message == "" {
		// 非 JSON（网关 HTML 等）兜底：留一段原文便于排查。
		e.Message = strings.TrimSpace(string(body))
		if len(e.Message) > 200 {
			e.Message = e.Message[:200] + "…"
		}
	}
	return e
}

// parseRetryAfter 解析 Retry-After（秒数或 HTTP-date）；解析不出返回 0（走指数退避）。
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
