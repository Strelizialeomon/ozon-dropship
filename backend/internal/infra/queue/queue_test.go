package queue

import (
	"testing"
	"time"
)

// 回归：退避差一位——asynq 首次失败传 n=0，过去 1<<uint(n-1) 下溢成 0，
// 第一次重试会「立刻重发」。首次必须等满一个 base。
func TestRetryDelay(t *testing.T) {
	base := time.Second
	cases := []struct {
		n    int
		want time.Duration
	}{
		{0, base},           // 首次重试：1×base（曾错成 0）
		{1, 2 * base},       // 第二次：2×base
		{2, 4 * base},       // 第三次：4×base
		{-1, base},          // 防御：负数按 0 处理
		{99, 5 * time.Minute}, // 封顶 5 分钟
	}
	for _, c := range cases {
		if got := retryDelay(base, c.n); got != c.want {
			t.Errorf("retryDelay(1s, %d) = %s, want %s", c.n, got, c.want)
		}
	}
}
