//go:build integration

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/auth"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/notify"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/middleware"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type testEnv struct {
	engine   *gin.Engine
	db       *gorm.DB
	rdb      *redis.Client
	authRepo *auth.Repo
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithLimiter(t, nil)
}

func newTestEnvWithLimiter(t *testing.T, lim *auth.LoginLimiter) *testEnv {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake: %v", err)
	}
	gin.SetMode(gin.TestMode)

	db := testutil.MySQL(t)
	testutil.Truncate(t, db)
	rdb := testutil.Redis(t)
	v := testutil.Vault(t)
	rec := audit.New(db)

	addr := os.Getenv(testutil.EnvRedis)
	q := queue.New(queue.Config{RedisAddr: addr, RedisDB: 15})
	t.Cleanup(q.Shutdown)

	notifier := notify.New(notify.Config{})
	_ = notifier

	shopRepo := store.NewRepo(db)
	credSvc := store.NewCredentialService(db, v, rec)
	sm := middleware.NewSessionManager(rdb, middleware.SessionConfig{
		CookieName: "test_session",
		Lifetime:   time.Hour,
	})
	authRepo := auth.NewRepo(db)

	engine, err := Setup(Deps{
		Mode:        "debug",
		Session:     sm,
		ResolveUser: authRepo.Resolve,
		Audit:       rec,
		Auth:        auth.NewHandlerWithLimiter(authRepo, sm, rec, lim),
		Stores:      store.NewHandler(shopRepo, rec),
		Credentials: store.NewCredentialHandler(credSvc, rec),
		System:      store.NewSystemHandler(shopRepo, q.Inspector()),
		Queue:       q,
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	return &testEnv{engine: engine, db: db, rdb: rdb, authRepo: authRepo}
}

func (e *testEnv) createUser(t *testing.T, name, role string) *auth.User {
	t.Helper()
	hash, err := auth.HashPassword("password-123")
	if err != nil {
		t.Fatal(err)
	}
	u := &auth.User{
		ID:           snowflake.GenStringID(),
		Name:         name,
		PasswordHash: hash,
		Role:         role,
		Status:       auth.StatusActive,
	}
	if err := e.authRepo.Create(context.Background(), u); err != nil {
		t.Fatalf("建用户: %v", err)
	}
	return u
}

func (e *testEnv) do(t *testing.T, req *http.Request, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) login(t *testing.T, name string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "password": "password-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := e.do(t, req, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Fatalf("登录业务失败: %s", rec.Body.String())
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "test_session" {
			return ck
		}
	}
	t.Fatalf("登录没有下发会话 Cookie（响应头: %v）", rec.Result().Header)
	return nil
}

// 验收：会话在 Redis；踢人（禁用账号）后旧会话立即失效。
func TestSessionInRedisAndKickTakesEffectImmediately(t *testing.T) {
	env := newTestEnv(t)
	u := env.createUser(t, "kickme", middleware.RoleAdmin)

	cookie := env.login(t, "kickme")

	// 1) 会话确实在 Redis（不是内存）。
	keys, err := env.rdb.Keys(context.Background(), "session:*").Result()
	if err != nil {
		t.Fatalf("读 Redis: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("登录后 Redis 里应有会话键")
	}

	// 2) 带会话可访问。
	rec := env.do(t, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/auth/me 应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}

	// 3) 踢人：禁用账号 → 同一个 Cookie 立即 401（不是等过期）。
	if err := env.db.Model(&auth.User{}).Where("id = ?", u.ID).Update("status", auth.StatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("踢人后应立即 401，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// 验收：operator 调 admin 接口返回 403；未登录 401；登出后会话失效。
func TestRoleGateAndLogout(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "boss", middleware.RoleAdmin)
	env.createUser(t, "worker", middleware.RoleOperator)

	// 未登录 → 401。
	rec := env.do(t, httptest.NewRequest(http.MethodGet, "/api/stores", nil), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，实际 %d", rec.Code)
	}

	// operator → admin 接口 403。
	opCookie := env.login(t, "worker")
	rec = env.do(t, httptest.NewRequest(http.MethodGet, "/api/admin/queues", nil), opCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator 调 admin 接口应 403，实际 %d: %s", rec.Code, rec.Body.String())
	}

	// admin → 200。
	adminCookie := env.login(t, "boss")
	rec = env.do(t, httptest.NewRequest(http.MethodGet, "/api/admin/queues", nil), adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 调监控页应 200，实际 %d", rec.Code)
	}

	// 登出后旧 Cookie 失效。
	rec = env.do(t, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil), adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("登出应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), adminCookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("登出后应 401，实际 %d", rec.Code)
	}
}

// 验收：所有写接口都写审计（含操作者与结果），读接口不写。
func TestWriteOpsAreAudited(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "boss2", middleware.RoleAdmin)
	cookie := env.login(t, "boss2")

	// 写 1：建店
	body := bytes.NewReader([]byte(`{"name":"店1","mode":"rfbs","client_id":"c1"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/stores", body)
	req.Header.Set("Content-Type", "application/json")
	rec := env.do(t, req, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":0`) {
		t.Fatalf("建店失败: %d %s", rec.Code, rec.Body.String())
	}

	// 写 2：建凭据
	body = bytes.NewReader([]byte(`{"kind":"alibaba_token","payload":{"access_token":"tok-1234567890"}}`))
	req = httptest.NewRequest(http.MethodPost, "/api/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	rec = env.do(t, req, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":0`) {
		t.Fatalf("建凭据失败: %d %s", rec.Code, rec.Body.String())
	}

	// 读：不应产生审计。
	req = httptest.NewRequest(http.MethodGet, "/api/stores", nil)
	env.do(t, req, cookie)

	var rows []audit.AuditLog
	if err := env.db.Order("at").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var writeOps, readOps int
	for _, r := range rows {
		if strings.HasPrefix(r.Action, "get ") {
			readOps++
		}
		if strings.HasPrefix(r.Action, "post ") {
			writeOps++
		}
		if r.Action == "post /api/stores" || r.Action == "post /api/credentials" {
			if r.Actor != "boss2" {
				t.Fatalf("审计应记操作者 boss2，实际 %q", r.Actor)
			}
			if !strings.Contains(r.Detail, `"code":0`) {
				t.Fatalf("审计应记结果 code，实际 %s", r.Detail)
			}
		}
	}
	if writeOps != 2 {
		t.Fatalf("应有 2 条写审计（建店 + 建凭据），实际 %d（全部: %+v）", writeOps, rows)
	}
	if readOps != 0 {
		t.Fatalf("读接口不应写审计，实际 %d", readOps)
	}

	// 凭据的领域审计（credential.create）也在。
	var n int64
	env.db.Model(&audit.AuditLog{}).Where("action = ?", "credential.create").Count(&n)
	if n != 1 {
		t.Fatalf("应有 1 条 credential.create 审计，实际 %d", n)
	}
}

// 回归：畸形 / 缺字段的登录请求也要留审计（曾被跳过 = 爆破者的无痕路径）；
// 超长用户名也要能落库（曾被 MySQL 1406 整条抹掉）。
func TestLoginFailureAuditedEvenWhenMalformed(t *testing.T) {
	env := newTestEnv(t)

	longName := strings.Repeat("x", 200)
	body := bytes.NewReader([]byte(`{"name":"` + longName + `"}`)) // 缺 password
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := env.do(t, req, nil)
	if !strings.Contains(rec.Body.String(), `"code":1001`) {
		t.Fatalf("缺字段应 1001，实际 %s", rec.Body.String())
	}

	var rows []audit.AuditLog
	if err := env.db.Where("action = ?", "auth.login.fail").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("畸形登录请求应留 1 条审计，实际 %d", len(rows))
	}
	if n := utf8.RuneCountInString(rows[0].Actor); n > 128 {
		t.Fatalf("actor 应被截到列宽（128 字符）内，实际 %d 字符", n)
	}
}

// 回归：登录失败限速——同 IP+用户名 失败超限后先拒（429），成功前不再校验密码。
func TestLoginThrottleBlocksAfterFailures(t *testing.T) {
	env := newTestEnvWithLimiter(t, auth.NewLoginLimiter(3, time.Minute))
	env.createUser(t, "target", middleware.RoleAdmin)

	attempt := func(pw string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"name": "target", "password": pw})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return env.do(t, req, nil)
	}

	for i := 0; i < 3; i++ {
		rec := attempt("wrong-password")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":2001`) {
			t.Fatalf("第 %d 次错误密码应 200/2001，实际 %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// 第 4 次：限速先于密码校验 → 429（哪怕密码是对的）。
	rec := attempt("password-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("超限后应 429，实际 %d %s", rec.Code, rec.Body.String())
	}
	var n int64
	env.db.Model(&audit.AuditLog{}).Where("action = ?", "auth.login.blocked").Count(&n)
	if n == 0 {
		t.Fatal("被限速的尝试也要留审计")
	}
}

// 验收：凭据接口只返回脱敏尾号。
func TestCredentialAPINeverReturnsPlaintext(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "boss3", middleware.RoleAdmin)
	cookie := env.login(t, "boss3")

	body := bytes.NewReader([]byte(`{"kind":"alibaba_token","payload":{"access_token":"super-secret-token-9999"}}`))
	req := httptest.NewRequest(http.MethodPost, "/api/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	rec := env.do(t, req, cookie)
	if !strings.Contains(rec.Body.String(), "****9999") {
		t.Fatalf("写入响应应含脱敏尾号: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/credentials", nil)
	rec = env.do(t, req, cookie)
	if strings.Contains(rec.Body.String(), "super-secret-token-9999") {
		t.Fatal("列表接口泄露了明文")
	}
	if !strings.Contains(rec.Body.String(), "****9999") {
		t.Fatalf("列表应含脱敏尾号: %s", rec.Body.String())
	}
}
