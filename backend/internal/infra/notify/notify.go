// Package notify 飞书机器人通知（总纲 §5.8 / 后端 ADR）：
// 同一错误去重、按分钟合并成一条发送；webhook 未配置时静默不启用。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
)

// Config 通知配置。
type Config struct {
	WebhookURL    string        // 留空 = 不启用
	Secret        string        // 签名密钥（飞书机器人「签名校验」）
	DedupeWindow  time.Duration // 同一 key 的去重窗口，默认 1 小时
	FlushInterval time.Duration // 合并发送周期，默认 1 分钟
}

// Notifier 去重 + 合并的通知器。Init(ctx) 起后台 goroutine，Flush() 退出前发完残余。
type Notifier struct {
	cfg    Config
	client *http.Client

	mu   sync.Mutex
	seen map[string]time.Time // key → 上次发送时间
	buf  []string             // 待发消息

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	started  bool
}

// maxBuffer 缓冲上限：上游失控时也不能把内存涨爆，超了直接丢并告警。
const maxBuffer = 200

// New 构造。
func New(cfg Config) *Notifier {
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = time.Hour
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = time.Minute
	}
	return &Notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		seen:   make(map[string]time.Time),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Enabled 是否启用。
func (n *Notifier) Enabled() bool { return n != nil && n.cfg.WebhookURL != "" }

// Init 启动合并发送 goroutine；必须先于 Notify 调用。未启用时空转。
func (n *Notifier) Init(_ context.Context) {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return
	}
	n.started = true
	n.mu.Unlock()

	go n.loop()
}

// Notify 报告一条消息。key 用于去重（同一 key 在窗口内只发一次）；
// 真正发送按 FlushInterval 合并成一条。
func (n *Notifier) Notify(key, title, text string) {
	if !n.Enabled() {
		return
	}
	now := time.Now()

	n.mu.Lock()
	if last, ok := n.seen[key]; ok && now.Sub(last) < n.cfg.DedupeWindow {
		n.mu.Unlock()
		return // 去重：同一档不重复
	}
	n.seen[key] = now
	// 顺手清理过期去重记录，避免 map 无限增长。
	for k, t := range n.seen {
		if now.Sub(t) >= n.cfg.DedupeWindow {
			delete(n.seen, k)
		}
	}
	msg := text
	if title != "" {
		msg = title + "\n" + text
	}
	if len(n.buf) >= maxBuffer {
		n.mu.Unlock()
		logger.Warnf("[notify] 缓冲已满(%d)，丢弃消息: %s", maxBuffer, key)
		return
	}
	n.buf = append(n.buf, msg)
	n.mu.Unlock()
}

// Flush 停止后台 goroutine 并把残余消息同步发出（优雅退出收尾）。
func (n *Notifier) Flush() {
	if n == nil {
		return
	}
	n.stopOnce.Do(func() { close(n.stop) })
	if n.started {
		<-n.done
	}
	n.flushOnce(context.Background())
}

func (n *Notifier) loop() {
	defer close(n.done)
	t := time.NewTicker(n.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-t.C:
			n.flushOnce(context.Background())
		}
	}
}

// flushOnce 把当前缓冲合并成一条发出；失败则把内容放回缓冲，下一轮重试
//（缓冲有上限，持续失败不会无限堆积；进程退出前的最后一次 Flush 失败只记日志）。
func (n *Notifier) flushOnce(ctx context.Context) {
	n.mu.Lock()
	if len(n.buf) == 0 {
		n.mu.Unlock()
		return
	}
	text := strings.Join(n.buf, "\n\n")
	n.buf = nil
	n.mu.Unlock()

	if err := n.send(ctx, text); err != nil {
		logger.Errorf("[notify] 发送失败（下轮重试）: %v", err)
		n.mu.Lock()
		if len(n.buf) < maxBuffer {
			n.buf = append([]string{text}, n.buf...)
		}
		n.mu.Unlock()
	}
}

// send 按飞书自定义机器人协议发送（含签名）。
// body: {"timestamp","sign","msg_type":"text","content":{"text":...}}
// 签名规则：stringToSign = timestamp + "\n" + secret，HMAC-SHA256(key=stringToSign, data="")，base64。
func (n *Notifier) send(ctx context.Context, text string) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := map[string]any{
		"timestamp": ts,
		"msg_type":  "text",
		"content":   map[string]string{"text": text},
	}
	if n.cfg.Secret != "" {
		body["sign"] = sign(ts, n.cfg.Secret)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("飞书返回 HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	// 飞书失败也回 HTTP 200，靠 body 里的 code 判——只看状态码会把
	//「签名错 / 关键词不匹配」当成功，告警就静默消失了。
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(respBody, &r) == nil && r.Code != 0 {
		return fmt.Errorf("飞书返回 code=%d: %s", r.Code, r.Msg)
	}
	return nil
}

// sign 飞书自定义机器人签名。
func sign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
