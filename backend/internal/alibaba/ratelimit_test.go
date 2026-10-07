package alibaba

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// jsonFirstAttempt 包装 handler：前 n 次尝试回指定 body，之后回该接口的 fixture。
func jsonFirstAttempt(t *testing.T, apiName string, n int, body string) func(w http.ResponseWriter, r *http.Request, gotAPI string, attempt int) {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request, gotAPI string, attempt int) {
		w.Header().Set("Content-Type", "application/json")
		if gotAPI == apiName && attempt <= n {
			_, _ = w.Write([]byte(body))
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", fixtureByAPI[gotAPI]))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}
}

// TestBusinessErrorNotRetried 业务拒绝（库存不足）：不重试、错误带码带文案。
func TestBusinessErrorNotRetried(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.createOrder.preview", 99,
			`{"success":false,"errorCode":"500_004","errorMessage":"库存不足"}`)
	})

	_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err == nil {
		t.Fatal("应当报错")
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "500_004" || ae.Message != "库存不足" {
		t.Fatalf("错误不对: %v（%#v）", err, ae)
	}
	if got := env.stub.count("alibaba.createOrder.preview"); got != 1 {
		t.Fatalf("业务拒绝被重试了 %d 次", got)
	}
}

// TestRateLimitCodeRetried 模拟 1688 超限错误：按退避重试后成功（验收第 4 条的模拟部分）。
// 候选错误码是【未验】占位，实测后回填（见 errors.go）。
func TestRateLimitCodeRetried(t *testing.T) {
	for _, code := range []string{"429", "RATE_LIMIT", "SP_RATE_LIMIT"} {
		t.Run(code, func(t *testing.T) {
			env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
				stub.handler = jsonFirstAttempt(t, "alibaba.createOrder.preview", 1,
					`{"errorCode":"`+code+`","errorMessage":"调用频率超限，请稍后重试"}`)
			})

			res, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
			if err != nil {
				t.Fatalf("退避后应成功: %v", err)
			}
			if len(res.OrderPreviews) != 1 {
				t.Fatalf("结果不对: %+v", res)
			}
			if got := env.stub.count("alibaba.createOrder.preview"); got != 2 {
				t.Fatalf("尝试次数 = %d, want 2", got)
			}
		})
	}
}

// TestHTTP429HonorsRetryAfter HTTP 429 + Retry-After：按其等待（秒级），再重试成功。
func TestHTTP429HonorsRetryAfter(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, attempt int) {
			if apiName == "alibaba.createOrder.preview" && attempt == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"errorCode":"429"}`))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	start := time.Now()
	res, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	if err != nil {
		t.Fatalf("Retry-After 后应成功: %v", err)
	}
	if len(res.OrderPreviews) != 1 {
		t.Fatal("结果不对")
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("没有按 Retry-After 等待：只过了 %s", elapsed)
	}
	if got := env.stub.count("alibaba.createOrder.preview"); got != 2 {
		t.Fatalf("尝试次数 = %d, want 2", got)
	}
}

// TestGateway5xxExhaustsRetries 网关 5xx：重试用尽后返回 *ratelimit.ExhaustedError。
func TestGateway5xxExhaustsRetries(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
			if apiName == "alibaba.createOrder.preview" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("bad gateway"))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	var ex *ratelimit.ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("应为 ExhaustedError，得到 %v", err)
	}
	if got := env.stub.count("alibaba.createOrder.preview"); got != 3 {
		t.Fatalf("尝试次数 = %d, want 3（1 次 + 重试 2 次）", got)
	}
}

// TestBadResponseShapeNotRetried 响应缺关键字段：不重试、错误码 BAD_RESPONSE。
func TestBadResponseShapeNotRetried(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = jsonFirstAttempt(t, "alibaba.createOrder.preview", 99, `{"success":true}`)
	})

	_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "BAD_RESPONSE" {
		t.Fatalf("错误不对: %v", err)
	}
	if got := env.stub.count("alibaba.createOrder.preview"); got != 1 {
		t.Fatalf("被重试了 %d 次", got)
	}
}

// TestHTTP4xxNotRetried 4xx（签名/权限类）：不重试。
func TestHTTP4xxNotRetried(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
			if apiName == "alibaba.createOrder.preview" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("forbidden"))
				return
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	var ae *APIError
	if !errors.As(err, &ae) || ae.HTTP != http.StatusForbidden {
		t.Fatalf("错误不对: %v", err)
	}
	if got := env.stub.count("alibaba.createOrder.preview"); got != 1 {
		t.Fatalf("被重试了 %d 次", got)
	}
}
