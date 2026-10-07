// Package queue 是 asynq 任务队列的统一装配（总纲 §5.5）：
// 任务队列 worker 与 HTTP 服务同进程；附带「补投扫描」框架——
// 写库与入队不在同一事务，业务记录停在「待处理」超时的，由扫描重新入队。
//
// 业务包（S1-D 等）用 RegisterRescan 注册自己的超时规则；任务一律带幂等键
// （asynq TaskID），重复入队自动去重、重复执行无副作用。
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
)

// Config 队列配置。
type Config struct {
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	Concurrency     int           // worker 并发数
	RescanInterval  time.Duration // 补投扫描默认周期（5 分钟）
	RetryMaxRetries int           // 任务失败重试上限（默认 3）
	RetryBackoff    time.Duration // 重试退避基数
	// OnTaskError 任务最终失败（重试用尽、将进归档）或补投扫描出错时的钩子。
	// 装配层把它接到 notify；不接就是只记日志。
	OnTaskError func(kind, detail string)
}

// RescanRule 一条补投规则：业务包提供「找出停在待处理且超时的记录」的查询函数。
type RescanRule struct {
	Name string
	// Timeout 超过这个时长仍停在待处理 = 卡住，需要补投。
	Timeout time.Duration
	// Interval 扫描周期；0 = 用全局默认（5 分钟）。
	Interval time.Duration
	// Rescan 返回需要重新入队的任务。cutoff = now - Timeout，业务查询按它筛。
	Rescan func(ctx context.Context, cutoff time.Time) ([]Task, error)
}

// Task 一条待入队任务。
type Task struct {
	Type           string // 形如 purchase:plan
	Payload        []byte // JSON
	IdempotencyKey string // 作为 asynq TaskID：重复入队自动去重
	Queue          string // 空 = default
}

// Client 队列客户端 + 服务端 + 扫描器（同进程）。
type Client struct {
	cfg      Config
	redisOpt asynq.RedisClientOpt

	client    *asynq.Client
	server    *asynq.Server
	scheduler *asynq.Scheduler
	mux       *asynq.ServeMux

	mu    sync.Mutex
	rules []RescanRule

	started  bool
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New 构造；不启动。装配序列：New → Handle/RegisterRescan/RegisterCron → Start。
func New(cfg Config) *Client {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	if cfg.RescanInterval <= 0 {
		cfg.RescanInterval = 5 * time.Minute
	}
	if cfg.RetryMaxRetries < 0 {
		cfg.RetryMaxRetries = 0
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Second
	}
	opt := asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}

	c := &Client{
		cfg:      cfg,
		redisOpt: opt,
		client:   asynq.NewClient(opt),
		mux:      asynq.NewServeMux(),
		stop:     make(chan struct{}),
	}

	c.server = asynq.NewServer(opt, asynq.Config{
		Concurrency: cfg.Concurrency,
		RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
			// n 是已重试次数（1 起）：base × 2^(n-1)，上限 5 分钟
			d := cfg.RetryBackoff * time.Duration(1<<uint(n-1))
			if d > 5*time.Minute {
				d = 5 * time.Minute
			}
			return d
		},
		ErrorHandler: errorHandler{c: c},
	})

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		shanghai = time.FixedZone("CST", 8*3600)
	}
	c.scheduler = asynq.NewScheduler(opt, &asynq.SchedulerOpts{Location: shanghai})
	return c
}

// RedisConnOpt 暴露 Redis 连接参数（asynqmon 监控页要用）。
func (c *Client) RedisConnOpt() asynq.RedisClientOpt { return c.redisOpt }

// Inspector asynq 检查器（/api/system 读队列积压与失败任务）。
func (c *Client) Inspector() *asynq.Inspector { return asynq.NewInspector(c.redisOpt) }

// Handle 注册任务处理器（Start 之前调用）。
func (c *Client) Handle(pattern string, h asynq.HandlerFunc) { c.mux.Handle(pattern, h) }

// Enqueue 入队一条任务（自动带默认重试上限）。
func (c *Client) Enqueue(ctx context.Context, t Task, opts ...asynq.Option) error {
	base := []asynq.Option{asynq.MaxRetry(c.cfg.RetryMaxRetries)}
	if t.IdempotencyKey != "" {
		base = append(base, asynq.TaskID(t.IdempotencyKey))
	}
	if t.Queue != "" {
		base = append(base, asynq.Queue(t.Queue))
	}
	_, err := c.client.EnqueueContext(ctx, asynq.NewTask(t.Type, t.Payload), append(base, opts...)...)
	// 幂等：asynq 对「同 TaskID」不是静默丢弃而是报 ErrTaskIDConflict——
	// 对补投扫描来说这正说明任务已在队里，视作成功。
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		return fmt.Errorf("入队 %s 失败: %w", t.Type, err)
	}
	return nil
}

// RegisterRescan 注册补投规则。
func (c *Client) RegisterRescan(rule RescanRule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = append(c.rules, rule)
}

// RegisterCron 注册定时任务（cron 五段式，按北京时间解释）。
func (c *Client) RegisterCron(spec string, t Task) error {
	opts := []asynq.Option{asynq.MaxRetry(c.cfg.RetryMaxRetries)}
	if t.IdempotencyKey != "" {
		opts = append(opts, asynq.TaskID(t.IdempotencyKey))
	}
	_, err := c.scheduler.Register(spec, asynq.NewTask(t.Type, t.Payload), opts...)
	if err != nil {
		return fmt.Errorf("注册定时任务 %s 失败: %w", t.Type, err)
	}
	return nil
}

// Start 启动 worker、定时器与补投扫描（全部后台跑，不阻塞）。
func (c *Client) Start(_ context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = true
	rules := append([]RescanRule(nil), c.rules...)
	c.mu.Unlock()

	if err := c.server.Start(c.mux); err != nil {
		return fmt.Errorf("启动 asynq worker 失败: %w", err)
	}
	if err := c.scheduler.Start(); err != nil {
		return fmt.Errorf("启动 asynq 定时器失败: %w", err)
	}

	for _, rule := range rules {
		c.wg.Add(1)
		go c.rescanLoop(rule)
	}
	logger.L().Info("queue started", "rules", len(rules), "concurrency", c.cfg.Concurrency)
	return nil
}

// Shutdown 优雅退出：先停扫描（停止生产），再停 worker（跑完在途任务）。
// 幂等：重复调用只有第一次完整执行。
func (c *Client) Shutdown() {
	c.mu.Lock()
	started := c.started
	c.started = false
	c.mu.Unlock()

	c.stopOnce.Do(func() {
		if started {
			close(c.stop)
			c.wg.Wait()
			c.scheduler.Shutdown()
			c.server.Shutdown()
		}
		_ = c.client.Close()
		logger.L().Info("queue stopped")
	})
}

// rescanLoop 启动立即扫一次（重启即补投），随后按周期扫。
func (c *Client) rescanLoop(rule RescanRule) {
	defer c.wg.Done()
	interval := rule.Interval
	if interval <= 0 {
		interval = c.cfg.RescanInterval
	}

	scan := func() {
		if rule.Rescan == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), interval)
		defer cancel()
		cutoff := time.Now().Add(-rule.Timeout)
		tasks, err := rule.Rescan(ctx, cutoff)
		if err != nil {
			logger.Errorf("[queue] 补投扫描 %s 失败: %v", rule.Name, err)
			c.reportError("rescan", rule.Name+": "+err.Error())
			return
		}
		for _, t := range tasks {
			if err := c.Enqueue(ctx, t); err != nil {
				logger.Errorf("[queue] 补投入队失败 %s/%s: %v", rule.Name, t.Type, err)
				c.reportError("rescan", rule.Name+"/"+t.Type+": "+err.Error())
			}
		}
		if len(tasks) > 0 {
			logger.L().Info("rescan requeued", "rule", rule.Name, "count", len(tasks))
		}
	}

	scan() // 启动立即扫：进程重启后把卡住的补上
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			scan()
		}
	}
}

// errorHandler 适配 asynq.ErrorHandler 接口。
type errorHandler struct{ c *Client }

func (h errorHandler) HandleError(ctx context.Context, task *asynq.Task, err error) {
	h.c.onTaskError(ctx, task, err)
}

// onTaskError asynq 任务处理失败回调：重试用尽的往钩子上报（装配层接通知）。
func (c *Client) onTaskError(ctx context.Context, task *asynq.Task, err error) {
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	logger.Errorf("[queue] 任务失败 type=%s 已重试=%d/%d: %v", task.Type(), retried, maxRetry, err)
	if retried >= maxRetry {
		c.reportError("task", fmt.Sprintf("%s（重试 %d 次用尽）: %v", task.Type(), maxRetry, err))
	}
}

func (c *Client) reportError(kind, detail string) {
	if c.cfg.OnTaskError != nil {
		c.cfg.OnTaskError(kind, detail)
	}
}

// RedisPing 供启动自检：Redis 不通就别带着坏队列起服务。
func RedisPing(ctx context.Context, addr, password string, db int) error {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	defer func() { _ = rdb.Close() }()
	return rdb.Ping(ctx).Err()
}
