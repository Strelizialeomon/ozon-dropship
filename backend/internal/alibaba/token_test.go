package alibaba

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTokenAutoRefreshWritesBack 冷启动第一次调用即续期；新 token 回写凭据（验收第 2 条）。
func TestTokenAutoRefreshWritesBack(t *testing.T) {
	env := newTestEnv(t, nil)

	if _, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
		t.Fatalf("PreviewOrder: %v", err)
	}

	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("续期次数 = %d, want 1", got)
	}

	// 回写：Put 载荷带上新的 access/refresh；expiresAt 传 nil（保留库里的 refresh_token 到期时间）。
	env.creds.mu.Lock()
	puts, setExps := env.creds.puts, env.creds.setExps
	env.creds.mu.Unlock()
	if len(puts) != 1 || puts[0].kind != kindAlibabaToken {
		t.Fatalf("Put 记录不对: %+v", puts)
	}
	if puts[0].expiresAt != nil {
		t.Fatal("Put 的 expiresAt 应传 nil（保留原到期时间，避免抹掉告警锚点）")
	}
	if got := puts[0].payload["access_token"]; got != "6100newtokennewtokennewtoken0000" {
		t.Fatalf("回写 access_token = %q", got)
	}
	if got := puts[0].payload["refresh_token"]; got != "6100newrefreshnewrefreshnewrefresh0" {
		t.Fatalf("回写 refresh_token = %q", got)
	}

	// refresh_token_timeout（20270507120000+0800）用 SetExpiry 推进。
	if len(setExps) != 1 || setExps[0].expiresAt == nil {
		t.Fatalf("SetExpiry 记录不对: %+v", setExps)
	}
	if year := setExps[0].expiresAt.Year(); year != 2027 {
		t.Fatalf("refresh_token 到期时间解析不对: %v", setExps[0].expiresAt)
	}
	if setExps[0].lastVerifiedAt == nil {
		t.Fatal("SetExpiry 应带 lastVerifiedAt")
	}

	// 业务请求带的是新 token。
	req := env.stub.lastRequest("alibaba.createOrder.preview")
	if got := req.form.Get(tokenParam); got != "6100newtokennewtokennewtoken0000" {
		t.Fatalf("业务请求的 access_token = %q", got)
	}
	// 续期请求的形状：POST 到 system.oauth2/getToken，带 grant_type/client_id/client_secret/refresh_token。
	tokReq := env.stub.lastRequest("getToken")
	f := tokReq.form
	if f.Get("grant_type") != "refresh_token" || f.Get("client_id") != "1234567" ||
		f.Get("client_secret") != testAppSecret || f.Get("refresh_token") != "old-refresh" {
		t.Fatalf("续期请求参数不对: %v", f)
	}
	// 续期请求不带签名（【未验】口径，见 token.go；实测后若网关要求签名在这里会先炸出来）。
	if f.Get(signatureParam) != "" {
		t.Fatal("续期请求不应带签名（待实测确认）")
	}
	if !strings.Contains(tokReq.path, "/param2/1/system.oauth2/getToken/1234567") {
		t.Fatalf("续期路径不对: %s", tokReq.path)
	}
}

// TestTokenCachedAcrossCalls 缓存生效：连续调用只续期一次。
func TestTokenCachedAcrossCalls(t *testing.T) {
	env := newTestEnv(t, nil)
	for i := 0; i < 3; i++ {
		if _, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
			t.Fatalf("第 %d 次: %v", i+1, err)
		}
	}
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("续期次数 = %d, want 1", got)
	}
}

// TestTokenRefreshMargin 快到期的阈值：剩余 20 分钟用缓存，剩余 10 分钟内续期。
func TestTokenRefreshMargin(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	call := func() {
		t.Helper()
		if _, err := env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
			t.Fatalf("PreviewOrder: %v", err)
		}
	}

	call() // 第一次：冷启动续期
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("首次续期次数 = %d", got)
	}

	// expires_in=36000（10 小时）。走到 9 小时 45 分：now+10min 仍在有效期内 → 用缓存。
	env.clock.Advance(9*time.Hour + 45*time.Minute)
	call()
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("半小时内不该续期，续期次数 = %d", got)
	}

	// 再走 10 分钟到 9 小时 55 分：进入 10 分钟窗口 → 续期。
	env.clock.Advance(10 * time.Minute)
	call()
	if got := env.stub.count("getToken"); got != 2 {
		t.Fatalf("进入窗口应续期，续期次数 = %d, want 2", got)
	}
}

// TestRefreshFailureIdentifiable 续期失败返回可识别的错误（验收第 2 条）；
// refresh_token 失效类错误标 Reauth（供到期告警用）。
func TestRefreshFailureIdentifiable(t *testing.T) {
	t.Run("invalid_grant 需重新授权", func(t *testing.T) {
		env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
			stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
				if apiName == "getToken" {
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh_token 已失效"}`))
					return
				}
				b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
				_, _ = w.Write(b)
			}
		})

		_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
		var te *TokenError
		if !errors.As(err, &te) {
			t.Fatalf("应为 *TokenError，得到 %v", err)
		}
		if te.Op != "refresh" {
			t.Fatalf("Op = %q", te.Op)
		}
		if !IsReauthRequired(err) {
			t.Fatal("invalid_grant 应标为需重新授权")
		}
		if got := env.stub.count("alibaba.createOrder.preview"); got != 0 {
			t.Fatalf("token 都没拿到还打了业务接口 %d 次", got)
		}
	})

	t.Run("普通业务错不标 Reauth", func(t *testing.T) {
		env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
			stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, _ int) {
				if apiName == "getToken" {
					_, _ = w.Write([]byte(`{"errorCode":"400","errorMessage":"请求参数错误"}`))
					return
				}
				b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
				_, _ = w.Write(b)
			}
		})

		_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
		var te *TokenError
		if !errors.As(err, &te) {
			t.Fatalf("应为 *TokenError，得到 %v", err)
		}
		if IsReauthRequired(err) {
			t.Fatal("普通业务错不应标 Reauth")
		}
	})
}

// TestNoRefreshTokenUsesStoredAccess 没配 refresh_token 时：不续期，直接用库里的 access_token。
func TestNoRefreshTokenUsesStoredAccess(t *testing.T) {
	env := newTestEnv(t, func(creds *fakeCreds, _ *gatewayStub) {
		creds.payloads[kindAlibabaToken] = map[string]string{"access_token": "static-access"}
	})

	if _, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
		t.Fatalf("PreviewOrder: %v", err)
	}
	if got := env.stub.count("getToken"); got != 0 {
		t.Fatalf("没有 refresh_token 不该续期，续期次数 = %d", got)
	}
	if got := env.stub.lastRequest("alibaba.createOrder.preview").form.Get(tokenParam); got != "static-access" {
		t.Fatalf("access_token = %q", got)
	}
}

// TestMissingTokenReauth 库里没有 token：可识别错误 + 需授权。
func TestMissingTokenReauth(t *testing.T) {
	env := newTestEnv(t, func(creds *fakeCreds, _ *gatewayStub) {
		creds.payloads[kindAlibabaToken] = map[string]string{}
	})

	_, err := env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("应为 *TokenError，得到 %v", err)
	}
	if !IsReauthRequired(err) {
		t.Fatal("没配 token 应标为需授权")
	}
}

// TestConcurrentRefreshSingleFlight 并发调用只触发一次续期（单飞）。
func TestConcurrentRefreshSingleFlight(t *testing.T) {
	env := newTestEnv(t, func(_ *fakeCreds, stub *gatewayStub) {
		base := stub.handler
		_ = base
		stub.handler = func(w http.ResponseWriter, r *http.Request, apiName string, attempt int) {
			if apiName == "getToken" {
				time.Sleep(30 * time.Millisecond) // 放大竞态窗口
			}
			b, _ := os.ReadFile(filepath.Join("testdata", fixtureByAPI[apiName]))
			_, _ = w.Write(b)
		}
	})

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = env.client.PreviewOrder(context.Background(), PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发调用失败: %v", i, err)
		}
	}
	if got := env.stub.count("getToken"); got != 1 {
		t.Fatalf("并发续期了 %d 次, want 1", got)
	}
}

// TestAppCredsCachedWithTTL 应用密钥有内存缓存（TTL 内不重复读库，读库会写审计，
// 不能每次调用都读）；TTL 过期后重读（轮换不用重启进程）。
func TestAppCredsCachedWithTTL(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	call := func() {
		t.Helper()
		if _, err := env.client.PreviewOrder(ctx, PreviewOrderRequest{Address: testAddress(), CargoList: testCargo()}); err != nil {
			t.Fatalf("PreviewOrder: %v", err)
		}
	}

	call()
	call()
	env.creds.mu.Lock()
	got := env.creds.gets[kindAlibabaApp]
	env.creds.mu.Unlock()
	if got != 1 {
		t.Fatalf("应用密钥读取 %d 次, want 1（TTL 内应走缓存）", got)
	}

	env.clock.Advance(11 * time.Minute)
	call()
	env.creds.mu.Lock()
	got = env.creds.gets[kindAlibabaApp]
	env.creds.mu.Unlock()
	if got != 2 {
		t.Fatalf("TTL 过后应重读，读取 %d 次, want 2", got)
	}
}
