package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type captured struct {
	mu    sync.Mutex
	bodies []map[string]any
}

func (c *captured) handler(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	c.mu.Lock()
	c.bodies = append(c.bodies, body)
	c.mu.Unlock()
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"code":0}`))
}

func (c *captured) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.bodies))
	for _, b := range c.bodies {
		content, _ := b["content"].(map[string]any)
		t, _ := content["text"].(string)
		out = append(out, t)
	}
	return out
}

func newTestNotifier(t *testing.T, url, secret string) *Notifier {
	t.Helper()
	n := New(Config{
		WebhookURL:    url,
		Secret:        secret,
		DedupeWindow:  time.Hour,
		FlushInterval: time.Hour, // 测试里用 Flush 手动发，避免后台定时器抢跑
	})
	return n
}

// 同一 key 在窗口内只发一次。
func TestDedupeSameKey(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(cap.handler))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, "")
	n.Notify("credential:1:stage:14", "报警", "第一条")
	n.Notify("credential:1:stage:14", "报警", "重复的")
	n.Flush()

	texts := cap.texts()
	if len(texts) != 1 {
		t.Fatalf("同一 key 应只发一次，实际 %d 条: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "第一条") {
		t.Fatalf("发的应是第一条: %v", texts)
	}
}

// 不同 key 在同一个刷新周期内合并成一条发送。
func TestMergeDifferentKeys(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(cap.handler))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, "")
	n.Notify("k1", "报警", "消息一")
	n.Notify("k2", "报警", "消息二")
	n.Flush()

	texts := cap.texts()
	if len(texts) != 1 {
		t.Fatalf("两条不同消息应合并成一次发送，实际 %d 条: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "消息一") || !strings.Contains(texts[0], "消息二") {
		t.Fatalf("合并消息应包含两条内容: %v", texts[0])
	}
}

// 签名符合飞书规则：HMAC-SHA256(key = timestamp+"\n"+secret, data = "")，base64。
func TestSignMatchesFeishuRule(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(cap.handler))
	defer srv.Close()

	secret := "test-secret-key"
	n := newTestNotifier(t, srv.URL, secret)
	n.Notify("k1", "", "带签名的消息")
	n.Flush()

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.bodies) != 1 {
		t.Fatalf("应有 1 次发送，实际 %d", len(cap.bodies))
	}
	body := cap.bodies[0]
	ts, _ := body["timestamp"].(string)
	sign, _ := body["sign"].(string)
	if ts == "" || sign == "" {
		t.Fatalf("应有 timestamp 与 sign: %v", body)
	}
	mac := hmac.New(sha256.New, []byte(ts+"\n"+secret))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if sign != want {
		t.Fatalf("签名不符：got %s want %s", sign, want)
	}
}

// 未配置 webhook 时静默：不崩、不发。
func TestDisabledNotifierIsSilent(t *testing.T) {
	n := New(Config{})
	if n.Enabled() {
		t.Fatal("未配置 webhook 不应启用")
	}
	n.Notify("k", "t", "x")
	n.Flush() // 不 panic 即通过
}
