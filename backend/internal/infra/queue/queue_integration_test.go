//go:build integration

package queue

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

func testClient(t *testing.T, maxRetries int) *Client {
	t.Helper()
	addr := os.Getenv(testutil.EnvRedis)
	if addr == "" {
		t.Skipf("%s 未设置，跳过集成测试", testutil.EnvRedis)
	}
	return New(Config{
		RedisAddr:       addr,
		RedisDB:         15, // 与开发库隔开
		Concurrency:     2,
		RescanInterval:  50 * time.Millisecond,
		RetryMaxRetries: maxRetries,
		RetryBackoff:    5 * time.Millisecond,
	})
}

func insertStuckTask(t *testing.T, db *gorm.DB, id string, updatedAt time.Time) {
	t.Helper()
	err := db.Exec(
		"INSERT INTO purchase_tasks (id, order_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, "order-1", "pending", updatedAt, updatedAt,
	).Error
	if err != nil {
		t.Fatalf("插入卡住记录失败: %v", err)
	}
}

// 验收：worker 与（假想中的）HTTP 同进程启动；进程启动时补投扫描立即跑一次，
// 「停在待处理且超时」的记录被重新入队并执行。
func TestRescanRequeuesStuckRecords(t *testing.T) {
	db := testutil.MySQL(t)
	_ = testutil.Redis(t)
	testutil.Truncate(t, db, "purchase_tasks")

	insertStuckTask(t, db, "stuck-1", time.Now().UTC().Add(-2*time.Hour))

	q := testClient(t, 0)
	got := make(chan string, 16)
	q.Handle("purchase:plan", func(_ context.Context, task *asynq.Task) error {
		got <- string(task.Payload())
		return nil
	})
	q.RegisterRescan(RescanRule{
		Name:    "purchase",
		Timeout: time.Minute, // 超 1 分钟没动 = 卡住
		Rescan: func(_ context.Context, cutoff time.Time) ([]Task, error) {
			var ids []string
			if err := db.Table("purchase_tasks").
				Where("status = ? AND updated_at < ?", "pending", cutoff).
				Pluck("id", &ids).Error; err != nil {
				return nil, err
			}
			tasks := make([]Task, 0, len(ids))
			for _, id := range ids {
				tasks = append(tasks, Task{
					Type:           "purchase:plan",
					Payload:        []byte(`{"id":"` + id + `"}`),
					IdempotencyKey: "purchase:plan:" + id,
				})
			}
			return tasks, nil
		},
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer q.Shutdown()

	select {
	case payload := <-got:
		if !strings.Contains(payload, "stuck-1") {
			t.Fatalf("补投的任务载荷不对: %s", payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("补投扫描没有把卡住的记录重新入队")
	}
}

// 幂等键 = asynq TaskID：重复入队在任务还在队列里时自动去重。
func TestEnqueueIdempotencyKeyDedupes(t *testing.T) {
	_ = testutil.Redis(t)
	q := testClient(t, 0)
	defer q.Shutdown()

	ctx := context.Background()
	task := Task{Type: "demo", Payload: []byte(`{}`), IdempotencyKey: "same-key-1"}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("第一次入队: %v", err)
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("第二次入队（应被去重、不报错）: %v", err)
	}
	info, err := q.Inspector().GetQueueInfo("default")
	if err != nil {
		t.Fatalf("GetQueueInfo: %v", err)
	}
	if info.Pending != 1 {
		t.Fatalf("同幂等键应只留 1 条待处理，实际 %d", info.Pending)
	}
}

// 回归（曾判「严重」）：任务归档后同幂等键再也投不进去——
// 补投扫描必须能识别死记录、清掉后重投，否则卡住的业务记录永远补投不出去。
func TestEnqueueRevivesArchivedTask(t *testing.T) {
	_ = testutil.Redis(t)
	q := testClient(t, 0) // 0 次重试：失败即归档

	var shouldFail atomic.Bool
	shouldFail.Store(true)
	done := make(chan string, 4)
	q.Handle("revive", func(_ context.Context, task *asynq.Task) error {
		if shouldFail.Load() {
			return errors.New("先炸一次，让它进归档")
		}
		done <- string(task.Payload())
		return nil
	})
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer q.Shutdown()

	ctx := context.Background()
	task := Task{Type: "revive", Payload: []byte(`{"n":1}`), IdempotencyKey: "revive-key-1"}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("首次入队: %v", err)
	}

	// 等它进归档。
	deadline := time.Now().Add(10 * time.Second)
	for {
		info, err := q.Inspector().GetQueueInfo("default")
		if err == nil && info.Archived == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务没有进归档，前置条件不成立")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 修好之后用同一个幂等键补投：应当清掉死记录、重新执行。
	shouldFail.Store(false)
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("归档后同键补投应成功（清死记录重投），实际: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("归档后的任务没有被成功重投执行")
	}
}

// 任务失败且重试用尽 → 落归档（就是「失败任务」，/api/system 里给运维看的那栏）。
func TestExhaustedTaskGoesToArchived(t *testing.T) {
	_ = testutil.Redis(t)
	q := testClient(t, 0) // 0 次重试：失败即归档
	q.Handle("boom", func(_ context.Context, _ *asynq.Task) error {
		return errors.New("炸了")
	})
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer q.Shutdown()

	if err := q.Enqueue(context.Background(), Task{Type: "boom"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := q.Inspector().GetQueueInfo("default")
		if err == nil && info.Archived == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("失败任务没有落归档")
}
