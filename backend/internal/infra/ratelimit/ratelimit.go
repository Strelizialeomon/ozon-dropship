// Package ratelimit 是渠道限流的统一入口（总纲 §5.5）。
//
// 桶形状：
//   - Ozon：每店（Client-Id）× 每接口一个桶，另叠加每 Client-Id 总闸（默认 50 次/秒）；
//   - 1688：企业级凭据无店铺维度，按「应用 + 接口」建桶（限额官方未公开【未验】，实施期实测）。
//
// Do 是给渠道客户端（S1-B / S1-C）用的执行器：每次尝试先排队（Wait），
// 失败按「429 读 Retry-After → 否则指数退避」重试，重试上限用尽返回明确错误。
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// 主体范围：桶的命名空间。
const (
	ScopeOzon    = "ozon"
	ScopeAlibaba = "alibaba"
)

// SubjectConfig 一个主体范围（Ozon 店 / 1688 应用）的限额。
type SubjectConfig struct {
	// DefaultRPS 总闸：每秒请求数。
	DefaultRPS float64
	// Endpoints 单接口限额（键 = 接口名，值 = 每秒请求数）；未列出的接口只受总闸约束。
	Endpoints map[string]float64
}

// Config 全部限额。
type Config struct {
	Ozon    SubjectConfig
	Alibaba SubjectConfig
}

// RateLimitedError 上游明确限流（HTTP 429 / 1688 超限错误）。
// RetryAfter > 0 时按其等待；为 0 走指数退避。渠道客户端负责构造它。
type RateLimitedError struct {
	RetryAfter time.Duration
	Err        error
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("被限流（Retry-After %s）: %v", e.RetryAfter, e.Err)
	}
	return fmt.Sprintf("被限流: %v", e.Err)
}

func (e *RateLimitedError) Unwrap() error { return e.Err }

// PermanentError 不可重试的失败（业务错误、4xx 参数错等）。
// 渠道客户端把这类错误包一层 Permanent，Do 会立即返回、不再重试。
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent 包一个不可重试错误。
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// ExhaustedError 重试次数用尽的明确错误（带最后一次失败原因）。
type ExhaustedError struct {
	Retries int
	Last    error
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("重试 %d 次仍失败: %v", e.Retries, e.Last)
}

func (e *ExhaustedError) Unwrap() error { return e.Last }

// maxRetryAfter 单次等待上限：上游给个荒唐的 Retry-After 也不许把 goroutine 挂死。
const maxRetryAfter = 5 * time.Minute

// Registry 限流注册表（进程内单例，全通道共享）。
type Registry struct {
	cfg         Config
	maxRetries  int
	backoffBase time.Duration

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// New 构造注册表。maxRetries 为失败后允许的重试次数（默认 3，总纲 §5.5）；
// backoffBase 为指数退避基数（第一次重试等 backoffBase，第二次 2×，依此类推）。
func New(cfg Config, maxRetries int, backoffBase time.Duration) *Registry {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if backoffBase <= 0 {
		backoffBase = time.Second
	}
	return &Registry{
		cfg:         cfg,
		maxRetries:  maxRetries,
		backoffBase: backoffBase,
		buckets:     make(map[string]*rate.Limiter),
	}
}

// Wait 排队：拿到「总闸 + 该接口」两个令牌才返回；超额时阻塞（ctx 取消则退出）。
func (r *Registry) Wait(ctx context.Context, scope, subject, endpoint string) error {
	total, ep := r.limiters(scope, subject, endpoint)
	if total != nil {
		if err := total.Wait(ctx); err != nil {
			return fmt.Errorf("总闸排队失败: %w", err)
		}
	}
	if ep != nil {
		if err := ep.Wait(ctx); err != nil {
			return fmt.Errorf("接口限流排队失败: %w", err)
		}
	}
	return nil
}

// Do 限流 + 重试地执行 fn。
//
// 每次尝试前先 Wait 排队；失败分类：
//   - *RateLimitedError：有 Retry-After 按其等待，否则指数退避；
//   - *PermanentError：立即返回（不重试）；
//   - 其余错误：指数退避。
//
// 重试达到上限返回 *ExhaustedError（errors.As 可判），由调用方决定进异常池还是上抛。
func (r *Registry) Do(ctx context.Context, scope, subject, endpoint string, fn func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if err := r.Wait(ctx, scope, subject, endpoint); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		var perm *PermanentError
		if errors.As(err, &perm) {
			return err
		}
		if attempt == r.maxRetries {
			break
		}

		wait := r.backoff(attempt)
		var rle *RateLimitedError
		if errors.As(err, &rle) && rle.RetryAfter > 0 {
			wait = min(rle.RetryAfter, maxRetryAfter)
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
	return &ExhaustedError{Retries: r.maxRetries, Last: lastErr}
}

// backoff 第 attempt 次重试（从 0 数）的等待时长：base × 2^attempt。
func (r *Registry) backoff(attempt int) time.Duration {
	base := float64(r.backoffBase) * math.Pow(2, float64(attempt))
	if base > float64(maxRetryAfter) {
		base = float64(maxRetryAfter)
	}
	return time.Duration(base)
}

// limiters 取（必要时建）两个桶：总闸桶 + 接口桶。
func (r *Registry) limiters(scope, subject, endpoint string) (total, ep *rate.Limiter) {
	sc := r.subjectConfig(scope)
	totalKey := scope + "|" + subject
	epKey := totalKey + "|" + endpoint

	r.mu.Lock()
	defer r.mu.Unlock()

	if sc.DefaultRPS > 0 {
		total = r.buckets[totalKey]
		if total == nil {
			burst := int(math.Ceil(sc.DefaultRPS))
			if burst < 1 {
				burst = 1
			}
			total = rate.NewLimiter(rate.Limit(sc.DefaultRPS), burst)
			r.buckets[totalKey] = total
		}
	}
	if rps, ok := sc.Endpoints[endpoint]; ok && rps > 0 {
		ep = r.buckets[epKey]
		if ep == nil {
			// 单接口桶 burst 固定 1：接口级限额本就苛刻（如库存 30 秒 1 次），
			// 攒多发等于没限。
			ep = rate.NewLimiter(rate.Limit(rps), 1)
			r.buckets[epKey] = ep
		}
	}
	return total, ep
}

func (r *Registry) subjectConfig(scope string) SubjectConfig {
	if scope == ScopeAlibaba {
		return r.cfg.Alibaba
	}
	return r.cfg.Ozon
}

// sleepCtx 可取消的等待。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
