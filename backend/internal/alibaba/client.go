package alibaba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// Config 客户端配置。
type Config struct {
	// BaseURL 网关根地址；默认 https://gw.open.1688.com/openapi（测试指到 httptest）。
	BaseURL string
	// Credentials 企业级凭据读写（1688 应用密钥与买家 token 都存在 credentials 表，
	// store_id 为空 = 企业级）。装配层用 store.CredentialService 适配后注入。
	Credentials CredentialStore
	// Registry S1-A 的限流注册表：1688 走 ScopeAlibaba 企业级桶（总纲 §5.5）。
	Registry *ratelimit.Registry
	// HTTPClient 可选；默认 30 秒超时。
	HTTPClient *http.Client
	// Now 可选；注入时钟（测试用），默认 time.Now。
	Now func() time.Time
}

// Client 1688 买家侧客户端。构造一次全进程共用（内部有 token 缓存与限流桶，
// 请求并发安全）。
type Client struct {
	baseURL  string
	registry *ratelimit.Registry
	http     *http.Client
	tokens   *tokenManager
	now      func() time.Time
}

const (
	defaultBaseURL = "https://gw.open.1688.com/openapi"
	// subjectApp 1688 限流主体：企业级凭据没有店铺维度，用固定串（S1 父 issue #5 契约）。
	subjectApp         = "app"
	defaultHTTPTimeout = 30 * time.Second
	// responseLimit 响应体读取上限，防网关/代理回超大内容把内存打满。
	responseLimit = 8 << 20
	// gatewayTimeLayout 传给 1688 的日期参数格式（createStartTime / createEndTime）。
	gatewayTimeLayout = "2006-01-02 15:04:05"
)

// NewClient 构造客户端。
func NewClient(cfg Config) (*Client, error) {
	if cfg.Credentials == nil {
		return nil, errors.New("alibaba: 未配置 Credentials（1688 凭据读写）")
	}
	if cfg.Registry == nil {
		return nil, errors.New("alibaba: 未配置 Registry（限流器）")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultHTTPTimeout}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	c := &Client{
		baseURL:  base,
		registry: cfg.Registry,
		http:     hc,
		now:      now,
	}
	c.tokens = &tokenManager{client: c, creds: cfg.Credentials, now: now}
	return c, nil
}

// api 一个 1688 接口的三元组。
type api struct {
	namespace string
	name      string
	version   int
}

// url 完整请求地址：{base}/param2/{version}/{namespace}/{name}/{appKey}。
func (a api) url(base, appKey string) string {
	return base + "/" + signPath(a.version, a.namespace, a.name, appKey)
}

// 本包用到的接口。
var (
	apiPreview           = api{"com.alibaba.trade", "alibaba.createOrder.preview", 1}
	apiFastCreateOrder   = api{"com.alibaba.trade", "alibaba.trade.fastCreateOrder", 1}
	apiGetBuyerView      = api{"com.alibaba.trade", "alibaba.trade.get.buyerView", 1}
	apiGetBuyerOrderList = api{"com.alibaba.trade", "alibaba.trade.getBuyerOrderList", 1}
	apiLogisticsInfos    = api{"com.alibaba.logistics", "alibaba.trade.getLogisticsInfos.buyerView", 1}
	apiLogisticsTrace    = api{"com.alibaba.logistics", "alibaba.trade.getLogisticsTraceInfo.buyerView", 1}
	apiGetToken          = api{"system.oauth2", "getToken", 1}
)

// do 执行一次带限流与重试的接口调用：
// 走 S1-A 的限流器（排队 → 失败按 429/Retry-After 或指数退避重试，上限默认 3 次），
// 每次尝试都重新取 token、重算签名（重试时时间戳自然刷新）。
//
// handle 拿到响应体后做「解错误 → 解载荷」：业务拒绝要包一层 ratelimit.Permanent
// （不重试），超限错误包 ratelimit.RateLimitedError（退避重试），见 errors.go 的 fail()。
func (c *Client) do(ctx context.Context, a api, params map[string]any, auth bool, handle func(body []byte) error) error {
	return c.registry.Do(ctx, ratelimit.ScopeAlibaba, subjectApp, a.name, func(ctx context.Context) error {
		body, err := c.post(ctx, a, params, auth)
		if err != nil {
			return err
		}
		return handle(body)
	})
}

// post 组装参数（取凭据 → 签名）并发一次 POST，把 HTTP 层失败翻译成重试语义。
func (c *Client) post(ctx context.Context, a api, params map[string]any, auth bool) ([]byte, error) {
	appKey, appSecret, err := c.tokens.appCreds(ctx)
	if err != nil {
		// 取凭据失败（保险箱没启用/凭据没配）重试无意义。
		return nil, ratelimit.Permanent(err)
	}

	form := url.Values{}
	for k, v := range params {
		if v == nil {
			continue
		}
		s, err := encodeParam(v)
		if err != nil {
			return nil, fmt.Errorf("alibaba %s: 参数 %s: %w", a.name, k, err)
		}
		form.Set(k, s)
	}
	if auth {
		tok, err := c.tokens.token(ctx)
		if err != nil {
			return nil, ratelimit.Permanent(err)
		}
		form.Set(tokenParam, tok)
	}
	form.Set(timestampParam, strconv.FormatInt(c.now().UnixMilli(), 10))

	flat := make(map[string]string, len(form)+1)
	for k := range form {
		flat[k] = form.Get(k)
	}
	form.Set(signatureParam, sign(signPath(a.version, a.namespace, a.name, appKey), appSecret, flat))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(c.baseURL, appKey), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alibaba %s: 请求失败: %w", a.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	if err != nil {
		return nil, fmt.Errorf("alibaba %s: 读响应失败: %w", a.name, err)
	}

	// HTTP 层失败翻译成重试语义：429→退避重试、5xx→可重试、其余 4xx→不重试。
	if err := httpStatusError(a, resp.StatusCode, resp.Header.Get("Retry-After"), body); err != nil {
		return nil, err
	}
	return body, nil
}

// parseRetryAfter 解析 Retry-After 头，只认整数秒。
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// encodeParam 按网关约定渲染一个参数值：标量用原文，对象/数组用 JSON 字符串
// （addressParam、cargoParamList 这类嵌套结构都是整体一个 JSON 串放进表单）。
func encodeParam(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	case json.RawMessage:
		return string(t), nil
	case bool:
		return strconv.FormatBool(t), nil
	case time.Time:
		return t.Format(gatewayTimeLayout), nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64), nil
	}

	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalBody 解 JSON 响应体，失败带接口名。
func unmarshalBody(a api, body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("alibaba %s: 解响应失败: %w（body: %s）", a.name, err, truncateBody(body))
	}
	return nil
}
