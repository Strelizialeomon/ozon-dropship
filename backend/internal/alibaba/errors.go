package alibaba

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// rateLimitCodes 上游「超限」的候选错误码（小写比对）。
// 【未验】1688 买家侧接口的限额与超限错误形态官方未公开（总纲 §3.2 / §13.2），
// 这里按常见形态收了一组；HTTP 429 单独在 httpStatusError 里处理。
// 权限批下来实测出真实形态后回填（子 spec 验收第 4 条），实测结论贴 S1 父 issue。
var rateLimitCodes = map[string]bool{
	"429":               true,
	"rate_limit":        true,
	"ratelimit":         true,
	"rate_limited":      true,
	"sp_rate_limit":     true,
	"throttling":        true,
	"limit_exceeded":    true,
	"too_many_requests": true,
}

// isRateLimitCode 判断错误码是否为「超限」。
func isRateLimitCode(code string) bool {
	return rateLimitCodes[strings.ToLower(code)]
}

// fail 把业务失败的 *APIError 翻译成重试语义：
//   - 超限 → *ratelimit.RateLimitedError（按 Retry-After 或指数退避重试）；
//   - 其余业务拒绝 → ratelimit.Permanent（立即上抛，不重试——重试一个被拒的请求没有意义）。
func fail(ae *APIError) error {
	if isRateLimitCode(ae.Code) {
		return &ratelimit.RateLimitedError{Err: ae}
	}
	return ratelimit.Permanent(ae)
}

// httpStatusError 把 HTTP 状态码翻译成重试语义（post 与 token 续期共用）：
//   - 2xx：nil（交给上层解包体）；
//   - 429：*ratelimit.RateLimitedError，Retry-After 读整数秒（HTTP 日期格式忽略，走指数退避）；
//   - 5xx：可重试的普通错误（网关/代理故障）；
//   - 其余 4xx：ratelimit.Permanent（签名错、权限不足这类，重试不变好）。
func httpStatusError(a api, status int, retryAfter string, body []byte) error {
	switch {
	case status < 400:
		return nil
	case status == http.StatusTooManyRequests:
		return &ratelimit.RateLimitedError{
			RetryAfter: parseRetryAfter(retryAfter),
			Err:        &APIError{API: a.name, Code: strconv.Itoa(status), Message: "HTTP 429", HTTP: status, Body: truncateBody(body)},
		}
	case status >= 500:
		return &gatewayError{a: a, status: status, body: truncateBody(body)}
	default:
		return ratelimit.Permanent(&APIError{
			API: a.name, Code: strconv.Itoa(status), Message: http.StatusText(status),
			HTTP: status, Body: truncateBody(body),
		})
	}
}

// gatewayError 网关 5xx：可重试的临时故障。
type gatewayError struct {
	a      api
	status int
	body   string
}

func (e *gatewayError) Error() string {
	return "1688 " + e.a.name + ": 网关 " + strconv.Itoa(e.status) + ": " + e.body
}
