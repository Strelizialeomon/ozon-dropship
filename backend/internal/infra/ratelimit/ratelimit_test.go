package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testRegistry() *Registry {
	return New(Config{
		Ozon:    SubjectConfig{DefaultRPS: 1000, Endpoints: map[string]float64{"stocks": 25}},
		Alibaba: SubjectConfig{DefaultRPS: 1000},
	}, 3, 5*time.Millisecond)
}

// 同店同接口超额时排队：接口桶 burst=1、25 次/秒 → 第二次要等 ~40ms。
func TestWaitQueuesOnEndpointLimit(t *testing.T) {
	r := testRegistry()
	ctx := context.Background()

	start := time.Now()
	if err := r.Wait(ctx, ScopeOzon, "store-1", "stocks"); err != nil {
		t.Fatalf("第一次 Wait: %v", err)
	}
	first := time.Since(start)
	if first > 20*time.Millisecond {
		t.Fatalf("第一次不应排队，实际等了 %s", first)
	}

	start = time.Now()
	if err := r.Wait(ctx, ScopeOzon, "store-1", "stocks"); err != nil {
		t.Fatalf("第二次 Wait: %v", err)
	}
	if waited := time.Since(start); waited < 30*time.Millisecond {
		t.Fatalf("第二次应排队 ~40ms，实际 %s", waited)
	}
}

// 不同店铺各自有桶，不互相排队。
func TestWaitSeparateBucketsPerSubject(t *testing.T) {
	r := testRegistry()
	ctx := context.Background()
	start := time.Now()
	for _, subject := range []string{"store-1", "store-2", "store-3"} {
		if err := r.Wait(ctx, ScopeOzon, subject, "stocks"); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d > 30*time.Millisecond {
		t.Fatalf("不同店的桶应互相独立，实际共等 %s", d)
	}
}

// 429 + Retry-After：按其等待后重试。
func TestDoHonorsRetryAfter(t *testing.T) {
	r := testRegistry()
	calls := 0
	start := time.Now()
	err := r.Do(context.Background(), ScopeOzon, "store-1", "posting/list", func(_ context.Context) error {
		calls++
		if calls == 1 {
			return &RateLimitedError{RetryAfter: 40 * time.Millisecond, Err: errors.New("429")}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("重试后应当成功: %v", err)
	}
	if calls != 2 {
		t.Fatalf("应当调用 2 次，实际 %d", calls)
	}
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Fatalf("应按 Retry-After 等 40ms，实际 %s", d)
	}
}

// 重试 3 次用尽 → 明确错误（ExhaustedError），且总调用 4 次（初次 + 3 次重试）。
func TestDoExhaustsRetriesWithClearError(t *testing.T) {
	r := testRegistry()
	calls := 0
	err := r.Do(context.Background(), ScopeOzon, "store-1", "posting/list", func(_ context.Context) error {
		calls++
		return errors.New("网络抖动")
	})
	if calls != 4 {
		t.Fatalf("初次 + 重试 3 次 = 4 次调用，实际 %d", calls)
	}
	var ex *ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("用尽后应返回 *ExhaustedError，得到 %T: %v", err, err)
	}
	if ex.Retries != 3 {
		t.Fatalf("Retries 应为 3，实际 %d", ex.Retries)
	}
	if err.Error() == "" || ex.Last == nil {
		t.Fatal("错误应带最后一次失败原因")
	}
}

// Permanent 错误不重试。
func TestDoDoesNotRetryPermanent(t *testing.T) {
	r := testRegistry()
	calls := 0
	err := r.Do(context.Background(), ScopeOzon, "store-1", "posting/list", func(_ context.Context) error {
		calls++
		return Permanent(errors.New("参数错"))
	})
	if calls != 1 {
		t.Fatalf("Permanent 不应重试，实际调用 %d 次", calls)
	}
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("应透传 PermanentError，得到 %v", err)
	}
}

// ctx 取消时 Do 立即退出，不空转。
func TestDoStopsOnContextCancel(t *testing.T) {
	r := testRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := r.Do(ctx, ScopeOzon, "store-1", "posting/list", func(_ context.Context) error {
		calls++
		cancel()
		return errors.New("boom")
	})
	if err == nil || calls != 1 {
		t.Fatalf("取消后应立刻退出：err=%v calls=%d", err, calls)
	}
}
