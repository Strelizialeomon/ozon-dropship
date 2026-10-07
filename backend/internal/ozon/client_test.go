package ozon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// newTestClient 造一个指向 httptest server 的客户端（不连真实店）。
func newTestClient(t *testing.T, maxRetries int, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	limiter := ratelimit.New(ratelimit.Config{}, maxRetries, time.Millisecond)
	return New(limiter, Options{
		ClientID: "test-client",
		APIKey:   "test-key",
		Subject:  "shop-test",
		BaseURL:  srv.URL,
	})
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读 testdata/%s 失败: %v", name, err)
	}
	return b
}

func writeJSON(t *testing.T, w http.ResponseWriter, body []byte) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		t.Errorf("写响应失败: %v", err)
	}
}

// TestClient_SendsRequiredHeaders 每次请求都带认证头与 JSON 内容类型。
func TestClient_SendsRequiredHeaders(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != pathRoles {
			t.Errorf("请求应为 POST %s，实际 %s %s", pathRoles, r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Client-Id"); got != "test-client" {
			t.Errorf("Client-Id 应为 test-client，实际 %q", got)
		}
		if got := r.Header.Get("Api-Key"); got != "test-key" {
			t.Errorf("Api-Key 应为 test-key，实际 %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type 应为 application/json，实际 %q", got)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent 不应为空")
		}
		writeJSON(t, w, readTestdata(t, "getroles_response.json"))
	}
	c := newTestClient(t, 0, h)

	roles, err := c.GetRoles(context.Background())
	if err != nil {
		t.Fatalf("GetRoles 失败: %v", err)
	}
	if len(roles.Roles) == 0 {
		t.Fatal("应解析出 roles")
	}
}

// TestClient_429_RetryAfter 模拟 429 + Retry-After，经限流器等待后重试成功
// （S1-B 验收项）。
func TestClient_429_RetryAfter(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(t, w, []byte(`{"code":8,"message":"Too many requests"}`))
			return
		}
		writeJSON(t, w, readTestdata(t, "getroles_response.json"))
	}
	c := newTestClient(t, 3, h)

	start := time.Now()
	roles, err := c.GetRoles(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("429 后重试应成功，实际失败: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("应请求 2 次（1 次 429 + 1 次成功），实际 %d 次", got)
	}
	if elapsed < time.Second {
		t.Fatalf("Retry-After=1 应等待至少 1 秒，实际只等 %v", elapsed)
	}
	if len(roles.Roles) == 0 || roles.ExpiresAt.IsZero() {
		t.Fatal("重试成功后应解析出 roles 与 expires_at")
	}
}

// TestClient_429_WithoutRetryAfter 429 无 Retry-After 头时走指数退避（可配置的短基数）。
func TestClient_429_WithoutRetryAfter(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(t, w, []byte(`{"code":8,"message":"Too many requests"}`))
			return
		}
		writeJSON(t, w, readTestdata(t, "getroles_response.json"))
	}
	c := newTestClient(t, 3, h)

	if _, err := c.GetRoles(context.Background()); err != nil {
		t.Fatalf("应指数退避后重试成功: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("应请求 2 次，实际 %d 次", got)
	}
}

// TestClient_400_PermanentError 4xx（非 429）不重试，错误可解出 *APIError。
func TestClient_400_PermanentError(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, []byte(`{"code":3,"message":"invalid request","details":[{"typeUrl":"t","value":"v"}]}`))
	}
	c := newTestClient(t, 3, h)

	_, err := c.GetRoles(context.Background())
	if err == nil {
		t.Fatal("400 应返回错误")
	}
	var perm *ratelimit.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("4xx 应包成 *ratelimit.PermanentError，实际 %T: %v", err, err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("应能解出 *APIError，实际 %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != 3 || apiErr.Message != "invalid request" {
		t.Errorf("APIError 字段不符: %+v", apiErr)
	}
	if len(apiErr.Details) != 1 || apiErr.Details[0].TypeURL != "t" {
		t.Errorf("APIError.Details 解析不符: %+v", apiErr.Details)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("4xx 不应重试，实际请求 %d 次", got)
	}
}

// TestClient_500_Exhausted 5xx 重试到上限后返回 *ratelimit.ExhaustedError。
func TestClient_500_Exhausted(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(t, w, []byte(`{"code":13,"message":"internal"}`))
	}
	c := newTestClient(t, 2, h)

	_, err := c.GetRoles(context.Background())
	var ex *ratelimit.ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("重试用尽应返回 *ratelimit.ExhaustedError，实际 %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("maxRetries=2 应共请求 3 次，实际 %d 次", got)
	}
}

// TestClient_429_Exhausted 429 重试也有上限。
func TestClient_429_Exhausted(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, []byte(`{"code":8,"message":"Too many requests"}`))
	}
	c := newTestClient(t, 1, h)

	_, err := c.GetRoles(context.Background())
	var ex *ratelimit.ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("应返回 *ratelimit.ExhaustedError，实际 %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("maxRetries=1 应共请求 2 次，实际 %d 次", got)
	}
}

// TestClient_NonJSONError 非 JSON 错误响应（网关 HTML 等）兜底进 APIError.Message。
func TestClient_NonJSONError(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte("<html>Forbidden</html>")); err != nil {
			t.Errorf("写响应失败: %v", err)
		}
	}
	c := newTestClient(t, 0, h)

	_, err := c.GetRoles(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("应能解出 *APIError，实际 %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Message != "<html>Forbidden</html>" {
		t.Errorf("非 JSON 响应应保留原文: %+v", apiErr)
	}
}

// TestClient_NetworkError_Retries 网络层错误（连不上）也走重试。
func TestClient_NetworkError_Retries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	limiter := ratelimit.New(ratelimit.Config{}, 1, time.Millisecond)
	c := New(limiter, Options{ClientID: "x", APIKey: "y", BaseURL: srv.URL})
	srv.Close() // 直接关掉，制造连接失败

	_, err := c.GetRoles(context.Background())
	var ex *ratelimit.ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("网络错误应重试到用尽（*ratelimit.ExhaustedError），实际 %T: %v", err, err)
	}
}

// TestNew_PanicsOnNilLimiter 限流器必填：nil 直接 panic，防静默不限流。
func TestNew_PanicsOnNilLimiter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil, ...) 应 panic")
		}
	}()
	New(nil, Options{ClientID: "x"})
}

// TestClient_RejectsRedirect 3xx 不跟随：防止认证头被转发到别的域。
func TestClient_RejectsRedirect(t *testing.T) {
	var targetCalls int32
	var leakedAuth, leakedKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetCalls, 1)
		leakedAuth = r.Header.Get("Client-Id")
		leakedKey = r.Header.Get("Api-Key")
	}))
	t.Cleanup(target.Close)

	h := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/evil", http.StatusFound)
	}
	c := newTestClient(t, 0, h)

	_, err := c.GetRoles(context.Background())
	if err == nil {
		t.Fatal("3xx 不应被当成功")
	}
	if got := atomic.LoadInt32(&targetCalls); got != 0 {
		t.Fatalf("不应跟随重定向，目标站收到 %d 次请求（泄漏 Client-Id=%q Api-Key=%q）",
			got, leakedAuth, leakedKey)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
		t.Fatalf("应返回 *APIError 302（Permanent），实际 %T: %v", err, err)
	}
}

// TestClient_BodyTooLarge 响应超上限显式报错（不静默截断）。
func TestClient_BodyTooLarge(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		big := bytes.Repeat([]byte("a"), maxBodyBytes+1024)
		if _, err := w.Write(big); err != nil {
			t.Errorf("写大响应失败: %v", err)
		}
	}
	c := newTestClient(t, 0, h)

	_, err := c.GetRoles(context.Background())
	var perm *ratelimit.PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("超限应返回 *ratelimit.PermanentError（含明确报错），实际 %T: %v", err, err)
	}
}
func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Errorf("秒数解析错误: %v", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Errorf("0 应为 0（走退避）: %v", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("垃圾值应为 0: %v", got)
	}
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 31*time.Second {
		t.Errorf("HTTP-date 解析异常: %v", got)
	}
}
