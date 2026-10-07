// Package middleware 是横切中间件：恢复、请求日志（RID）、登录会话（scs + Redis）、
// 角色门（admin / operator）与写操作审计。
//
// 依赖方向（ADR-20261007-go-package-deps）：本包不认识任何业务域——
// 取用户状态的 UserResolver 由装配层注入（接口/函数放使用方）。
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/alexedwards/scs/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// gin.Context 里会话 / 用户的键。
const (
	CtxUserID   = "ctx_user_id"
	CtxUserName = "ctx_user_name"
	CtxUserRole = "ctx_user_role"

	// SessionKeyUserID scs 会话里存用户 ID 的键。
	SessionKeyUserID = "uid"

	// RoleAdmin / RoleOperator 两级角色（总纲 §8）。
	RoleAdmin    = "admin"
	RoleOperator = "operator"
)

// ---- 恢复 ----

// Recovery 兜住 panic：记日志、回 500（Recovery 必须最外层）。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Errorf("[panic] %s %s: %v", c.Request.Method, c.Request.URL.Path, recovered)
		utils.ServerError(c, "服务器内部错误", fmt.Errorf("%v", recovered))
		c.Abort()
	})
}

// ---- 请求日志（RID）----

// RequestLogger 每请求生成 4 字节 hex RID，写入 X-Request-ID 响应头与日志。
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := newRID()
		c.Header("X-Request-ID", rid)
		c.Set("rid", rid)

		start := time.Now()
		c.Next()

		logger.L().Info("http",
			"rid", rid,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"cost_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

func newRID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ---- 会话（scs + Redis）----

// SessionConfig 会话参数（由装配层从配置翻译过来）。
type SessionConfig struct {
	CookieName string
	Lifetime   time.Duration
	Secure     bool // release（HTTPS 之后）为 true
}

// NewSessionManager 构造 scs 会话管理器，会话存 Redis。
func NewSessionManager(rdb *redis.Client, cfg SessionConfig) *scs.SessionManager {
	sm := scs.New()
	sm.Store = &redisStore{client: rdb}
	sm.Lifetime = cfg.Lifetime
	sm.Cookie.Name = cfg.CookieName
	sm.Cookie.Path = "/"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cfg.Secure
	return sm
}

// redisStore 把 scs 的会话存进 Redis（键 session:<token>）。
// 自实现 30 行的 Store，避免为 scs 再引一个 Redis 客户端库（同类只留一个）。
type redisStore struct{ client *redis.Client }

const sessionKeyPrefix = "session:"

func (s *redisStore) Find(token string) ([]byte, bool, error) {
	b, err := s.client.Get(context.Background(), sessionKeyPrefix+token).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *redisStore) Commit(token string, b []byte, expiry time.Time) error {
	ttl := time.Until(expiry)
	if ttl <= 0 {
		return nil
	}
	return s.client.Set(context.Background(), sessionKeyPrefix+token, b, ttl).Err()
}

func (s *redisStore) Delete(token string) error {
	return s.client.Del(context.Background(), sessionKeyPrefix+token).Err()
}

// Session 把 scs 会话接进 gin。
//
// 为什么不用 sm.LoadAndSave：scs 靠它自己包装的 http.ResponseWriter，在「首次写响应之前」
// 提交会话并写 Set-Cookie；gin 的 handler 是往 c.Writer 写的，会绕过那层包装，
// 于是 Set-Cookie 落在响应已刷出之后、被 net/http 丢弃（典型症状：登录返回成功但没有 Cookie）。
// 这里用同样的提交时机自实现 gin 版包装——复用 scs 公开的 Load / Status / Commit /
// WriteSessionCookie，语义与 LoadAndSave 一致：
// Modified → 提交并写 Cookie；Destroyed → 写过期 Cookie。
func Session(sm *scs.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _ := c.Cookie(sm.Cookie.Name)
		ctx, err := sm.Load(c.Request.Context(), token)
		if err != nil {
			logger.Errorf("[session] 加载会话失败: %v", err)
			utils.ServerError(c, "会话加载失败", err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)

		cw := &commitWriter{ResponseWriter: c.Writer, sm: sm, ctx: ctx}
		c.Writer = cw
		c.Next()
		cw.commit() // 兜底：路由没写任何响应（404 / Abort 无输出）时也要提交
	}
}

// commitWriter 在首次写响应前提交会话（scs 语义的 gin 版）。
type commitWriter struct {
	gin.ResponseWriter
	sm   *scs.SessionManager
	ctx  context.Context
	done bool
}

func (w *commitWriter) commit() {
	if w.done {
		return
	}
	w.done = true
	switch w.sm.Status(w.ctx) {
	case scs.Modified:
		token, expiry, err := w.sm.Commit(w.ctx)
		if err != nil {
			logger.Errorf("[session] 提交会话失败: %v", err)
			return
		}
		w.sm.WriteSessionCookie(w.ctx, w, token, expiry)
	case scs.Destroyed:
		w.sm.WriteSessionCookie(w.ctx, w, "", time.Time{})
	}
}

func (w *commitWriter) WriteHeader(code int) {
	w.commit()
	w.ResponseWriter.WriteHeader(code)
}

func (w *commitWriter) WriteHeaderNow() {
	w.commit()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *commitWriter) Write(b []byte) (int, error) {
	w.commit()
	return w.ResponseWriter.Write(b)
}

func (w *commitWriter) WriteString(s string) (int, error) {
	w.commit()
	return w.ResponseWriter.WriteString(s)
}

// ---- 登录与角色 ----

// UserResolver 由装配层把 store/auth 域的用户查询接进来（接口放使用方）。
// 返回当前角色与是否可用（status = active）。
type UserResolver func(ctx context.Context, userID string) (name, role string, active bool, err error)

// RequireAuth 校验会话；每次请求都回源查用户状态——
// 所以「踢人」（禁用账号 / 清会话）后旧会话立即失效，不是 JWT 那种到期才算。
func RequireAuth(sm *scs.SessionManager, resolve UserResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		userID := sm.GetString(ctx, SessionKeyUserID)
		if userID == "" {
			utils.Unauthorized(c, "未登录或会话已失效")
			c.Abort()
			return
		}
		name, role, active, err := resolve(ctx, userID)
		if err != nil || !active {
			// 账号被禁用 / 已删除：顺手把残留会话清掉。
			_ = sm.Destroy(ctx)
			utils.Unauthorized(c, "账号不可用，请重新登录")
			c.Abort()
			return
		}
		// 操作者写进 ctx：审计（infra/audit）统一从这里取。
		c.Request = c.Request.WithContext(audit.WithActor(ctx, name))
		c.Set(CtxUserID, userID)
		c.Set(CtxUserName, name)
		c.Set(CtxUserRole, role)
		c.Next()
	}
}

// RequireAdmin 只放 admin（operator 调 admin 接口 → 403）。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if RoleOf(c) != RoleAdmin {
			utils.Forbidden(c, "需要管理员权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

// UserIDOf / UserNameOf / RoleOf 取当前登录用户。
func UserIDOf(c *gin.Context) string   { return c.GetString(CtxUserID) }
func UserNameOf(c *gin.Context) string { return c.GetString(CtxUserName) }
func RoleOf(c *gin.Context) string     { return c.GetString(CtxUserRole) }

// ---- 写操作审计 ----

// AuditWrite 给所有写接口（非 GET/HEAD/OPTIONS）留痕：谁、什么动作、对哪个路径、
// 结果如何（HTTP 状态 + 响应壳里的业务 code）。登录接口除外——它自己记（含失败）。
func AuditWrite(rec *audit.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		m := c.Request.Method
		if m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions {
			c.Next()
			return
		}
		if c.FullPath() == "/api/auth/login" {
			c.Next()
			return
		}

		capture := &bodyCapture{ResponseWriter: c.Writer, limit: 1024}
		c.Writer = capture
		c.Next()

		detail, _ := json.Marshal(map[string]any{
			"http_status": capture.Status(),
			"code":        capture.bizCode(),
			"query":       c.Request.URL.RawQuery,
		})
		actor := UserNameOf(c)
		if actor == "" {
			actor = "anonymous" // 未登录就发写请求：记 anonymous，别混进 system（后台任务才叫 system）
		}
		rec.Record(c.Request.Context(), audit.Entry{
			Actor:  actor,
			Action: strings.ToLower(m) + " " + c.FullPath(),
			Object: c.Request.URL.Path,
			Detail: string(detail),
		})
	}
}

// bodyCapture 截获响应体前 1KB，用来解析响应壳里的业务 code；其余透传。
type bodyCapture struct {
	gin.ResponseWriter
	limit int
	body  []byte
}

func (w *bodyCapture) Write(b []byte) (int, error) {
	if len(w.body) < w.limit {
		remain := w.limit - len(w.body)
		if remain > len(b) {
			remain = len(b)
		}
		w.body = append(w.body, b[:remain]...)
	}
	return w.ResponseWriter.Write(b)
}

func (w *bodyCapture) WriteString(s string) (int, error) {
	if len(w.body) < w.limit {
		remain := w.limit - len(w.body)
		if remain > len(s) {
			remain = len(s)
		}
		w.body = append(w.body, s[:remain]...)
	}
	return w.ResponseWriter.WriteString(s)
}

// bizCode 从响应壳解析业务 code；解不出（如非 JSON 响应）返回 -1。
func (w *bodyCapture) bizCode() int {
	var r struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.body, &r); err != nil {
		return -1
	}
	return r.Code
}
