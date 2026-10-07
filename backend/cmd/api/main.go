// api 是履约中台的主进程：HTTP 服务与 asynq worker 同进程（总纲 §4 / §5.5）。
//
// 装配序列（标准档）：配置 → 日志 → 雪花 → DB → Redis → 保险箱 → 通知 → 审计 →
// 队列（含补投扫描与定时任务）→ 各域 → 路由 → HTTP 服务 → 优雅退出。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/auth"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/catalog"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/config"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/db"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/notify"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/vault"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/middleware"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/purchase"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/router"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/shipment"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/redis/go-redis/v9"
)

const shutdownTimeout = 10 * time.Second

func main() {
	configPath := flag.String("config", "config", "配置目录路径")
	flag.Parse()

	// 1. 配置 → 日志（日志要早）。
	if err := config.Init(*configPath); err != nil {
		panic(err)
	}
	cfg := config.Get()
	logger.Init(cfg.Server.Mode)
	defer logger.Sync()
	logger.L().Info("starting", "mode", cfg.Server.Mode, "port", cfg.Server.Port)

	// 2. 雪花 ID
	if err := snowflake.Init(1); err != nil {
		logger.Fatalf("[main] 初始化雪花 ID 失败: %v", err)
	}

	// 3. DB
	if err := db.Init(cfg.Database.DSN()); err != nil {
		logger.Fatalf("[main] %v", err)
	}
	defer func() { _ = db.Close() }()

	// 4. Redis（队列 + 会话）
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() { _ = rdb.Close() }()
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		pingCancel()
		logger.Fatalf("[main] Redis 不可用: %v", err)
	}
	pingCancel()

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// 5. 凭据保险箱（未配置 = 静默不启用，见 vault 包注释）
	v, err := vault.New(cfg.Vault.CredentialsDir, cfg.Vault.MasterKeyFile)
	if err != nil {
		logger.Fatalf("[main] 保险箱初始化失败: %v", err)
	}
	if !v.Enabled() {
		logger.Warnf("[main] 保险箱未启用（vault.credentials_dir 未配置）：凭据接口将拒绝读写")
	}

	// 6. 通知（飞书）
	notifier := notify.New(notify.Config{
		WebhookURL:    cfg.Notify.WebhookURL,
		Secret:        cfg.Notify.Secret,
		DedupeWindow:  time.Duration(cfg.Notify.DedupeWindowMinutes) * time.Minute,
		FlushInterval: time.Duration(cfg.Notify.FlushSeconds) * time.Second,
	})
	notifier.Init(rootCtx)

	// 7. 审计
	auditRec := audit.New(db.DB)

	// 8. 队列 + 补投扫描框架
	q := queue.New(queue.Config{
		RedisAddr:       cfg.Redis.Addr,
		RedisPassword:   cfg.Redis.Password,
		RedisDB:         cfg.Redis.DB,
		Concurrency:     cfg.Queue.Concurrency,
		RescanInterval:  time.Duration(cfg.Queue.RescanSeconds) * time.Second,
		RetryMaxRetries: cfg.Queue.RetryMaxRetries,
		RetryBackoff:    time.Duration(cfg.Queue.RetryBackoffSeconds) * time.Second,
		OnTaskError: func(kind, detail string) {
			notifier.Notify("queue:"+kind+":"+detail, "后台任务失败", detail)
		},
	})

	// 渠道限流器：S1-B 起用，Ozon 客户端所有请求经它（S1-C 共用同一实例）。
	limiter := newRateLimiter(cfg)

	// 9. 各域装配
	shopRepo := store.NewRepo(db.DB)
	credSvc := store.NewCredentialService(db.DB, v, auditRec)
	shopHandler := store.NewHandler(shopRepo, auditRec)
	credHandler := store.NewCredentialHandler(credSvc, auditRec)
	systemHandler := store.NewSystemHandler(shopRepo, q.Inspector())

	// 凭据到期检查：每天北京时间 09:00（总纲 §13.1）；Ozon 侧顺带读 /v1/roles
	// 刷新到期时间（总纲 §5.8，适配见 cmd/api/ozon.go）。
	expiryChecker := store.NewExpiryChecker(credSvc, shopRepo, notifier, ozonRolesFetcherFactory(limiter))
	q.Handle(store.TaskTypeExpiryCheck, expiryChecker.Handle)
	if err := q.RegisterCron("0 9 * * *", queue.Task{Type: store.TaskTypeExpiryCheck}); err != nil {
		logger.Fatalf("[main] %v", err)
	}

	// S1-D 履约编排：catalog（货源映射）→ order（订单 + 异常池）→ purchase / shipment。
	catalogRepo := catalog.NewRepo(db.DB)
	catalogHandler := catalog.NewHandler(catalogRepo, auditRec)

	exceptions := order.NewExceptions(db.DB)
	// 总纲 §5.2「异常判定（自动进池 + 通知）」：异常写入同时走飞书，
	// 去重键 = 对象 + 码（notify 自己的窗口合并重复）。
	exceptions.SetNotifier(notifier)
	orderRepo := order.NewRepo(db.DB)
	relayRepo := order.NewRelayRepo(db.DB)
	// TODO(S1-B 合并后)：把 ozon 客户端适配成 order.PostingSourceFactory 接进第 5 参，
	//   拉单任务当前会明确报「Ozon 客户端未接入」；人工流程与其余接口不受影响。
	orderSvc := order.NewService(orderRepo, exceptions, shopRepo, credSvc, nil, q)
	orderHandler := order.NewHandler(orderRepo, orderSvc, auditRec)
	exceptionHandler := order.NewExceptionHandler(exceptions, auditRec)

	// TODO(S1-C 合并后)：把 alibaba 客户端适配成 purchase.BuyerClientFactory 接进第 6 参，
	//   自动执行器当前会报「1688 客户端未接入」（可转人工）。
	purchaseRepo := purchase.NewRepo(db.DB)
	purchaseSvc := purchase.NewService(purchaseRepo, orderSvc, relayRepo, catalogRepo, credSvc, nil, auditRec, q)
	purchaseHandler := purchase.NewHandler(purchaseRepo, purchaseSvc, auditRec)

	// TODO(S1-B 合并后)：把 ozon 客户端适配成 shipment.FulfillerFactory 接进第 5 参。
	shipmentRepo := shipment.NewRepo(db.DB)
	shipmentSvc := shipment.NewService(shipmentRepo, orderSvc, purchaseRepo, credSvc, nil, auditRec)
	shipmentHandler := shipment.NewHandler(shipmentSvc, relayRepo, auditRec)

	// S1-D 任务处理器与补投规则（总纲 §5.5）。
	q.Handle(order.TaskTypePollStores, orderSvc.HandlePollStores)
	q.Handle(order.TaskTypePoll, orderSvc.HandlePoll)
	q.Handle(order.TaskTypeSweep, orderSvc.HandleSweep)
	q.Handle(order.TaskTypePurchasePlan, purchaseSvc.HandlePlan)
	q.Handle(purchase.TaskTypeExecute, purchaseSvc.HandleExecute)
	q.Handle(purchase.TaskTypeClose, purchaseSvc.HandleClose)
	q.Handle(purchase.TaskTypeSweep, purchaseSvc.HandleSweep)
	q.RegisterRescan(orderSvc.RescanRule())
	for _, rule := range purchaseSvc.RescanRules() {
		q.RegisterRescan(rule)
	}
	// 定时任务（北京时间）：拉单扫描 5 分钟、超时异常扫描 10 分钟、国内段停滞扫描 30 分钟。
	for _, cron := range []struct {
		spec string
		typ  string
	}{
		{"*/5 * * * *", order.TaskTypePollStores},
		{"*/10 * * * *", order.TaskTypeSweep},
		{"*/30 * * * *", purchase.TaskTypeSweep},
	} {
		if err := q.RegisterCron(cron.spec, queue.Task{Type: cron.typ}); err != nil {
			logger.Fatalf("[main] %v", err)
		}
	}

	authRepo := auth.NewRepo(db.DB)
	sm := middleware.NewSessionManager(rdb, middleware.SessionConfig{
		CookieName: cfg.Session.CookieName,
		Lifetime:   cfg.Session.Lifetime(),
		Secure:     cfg.Server.Mode == "release",
	})
	authHandler := auth.NewHandler(authRepo, sm, auditRec)

	// 10. 路由
	r, err := router.Setup(router.Deps{
		Mode:        cfg.Server.Mode,
		Session:     sm,
		ResolveUser: authRepo.Resolve,
		Audit:       auditRec,
		Auth:        authHandler,
		Stores:      shopHandler,
		Credentials: credHandler,
		System:      systemHandler,

		Orders:        orderHandler,
		Exceptions:    exceptionHandler,
		PurchaseTasks: purchaseHandler,
		Catalog:       catalogHandler,
		Shipments:     shipmentHandler,

		Queue: q,
	})
	if err != nil {
		logger.Fatalf("[main] 路由装配失败: %v", err)
	}
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		logger.Fatalf("[main] 设置信任代理失败: %v", err)
	}

	// 11. 队列与 HTTP 同进程启动
	if err := q.Start(rootCtx); err != nil {
		logger.Fatalf("[main] 队列启动失败: %v", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logger.L().Info("http listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("[main] HTTP 服务异常退出: %v", err)
		}
	}()

	// 12. 优雅退出：SIGINT/SIGTERM → 先停 HTTP（不收新请求）→ 再停队列（跑完在途任务）→ 发完通知。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L().Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("[main] HTTP 优雅退出超时: %v", err)
	}
	q.Shutdown()
	notifier.Flush()
	rootCancel()
	logger.L().Info("stopped")
}
