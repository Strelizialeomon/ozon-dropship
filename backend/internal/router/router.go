// Package router 是唯一路由注册处：中间件顺序与各域路由在这里串起来。
package router

import (
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/auth"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/middleware"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/alexedwards/scs/v2"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynqmon"
)

// Deps 装配依赖（cmd/api 组装后传入）。
type Deps struct {
	Mode        string
	Session     *scs.SessionManager
	ResolveUser middleware.UserResolver
	Audit       *audit.Recorder

	Auth        *auth.Handler
	Stores      *store.Handler
	Credentials *store.CredentialHandler
	System      *store.SystemHandler

	Queue *queue.Client
}

// Setup 组装 gin 引擎。中间件顺序：Recovery 最外层 → 请求日志 → 会话 → 业务链。
func Setup(d Deps) (*gin.Engine, error) {
	if d.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.New()
	r.Use(
		middleware.Recovery(),
		middleware.RequestLogger(),
		middleware.Session(d.Session),
	)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	api := r.Group("/api")
	// 写操作统一留痕（登录接口除外，它自己记，含失败）。
	api.Use(middleware.AuditWrite(d.Audit))

	// 公开：登录
	d.Auth.RegisterPublic(api.Group("/auth"))

	// 登录后
	authed := api.Group("")
	authed.Use(middleware.RequireAuth(d.Session, d.ResolveUser))
	d.Auth.RegisterAuthed(authed.Group("/auth"))
	d.Stores.Register(authed)
	d.Credentials.Register(authed)
	d.System.Register(authed)

	// 仅管理员
	admin := authed.Group("/admin")
	admin.Use(middleware.RequireAdmin())
	if err := mountQueues(admin, d.Queue); err != nil {
		return nil, err
	}
	return r, nil
}

// mountQueues 挂 asynq 队列监控页（仅 admin 可见可进）。
func mountQueues(rg *gin.RouterGroup, q *queue.Client) error {
	mon := asynqmon.New(asynqmon.Options{
		RootPath:     "/api/admin/queues",
		RedisConnOpt: q.RedisConnOpt(),
	})
	rg.Any("/queues", gin.WrapH(mon))
	rg.Any("/queues/*any", gin.WrapH(mon))
	return nil
}
