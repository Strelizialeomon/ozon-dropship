//go:build integration

package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/notify"
)

// 验收：到期前 14 / 7 / 1 天各告警一次飞书，同一档不重复。
func TestExpiryAlertStagesFireOncePerStage(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	// 假飞书：收消息并计数。
	var mu sync.Mutex
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		texts = append(texts, body.Content.Text)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	notifier := notify.New(notify.Config{
		WebhookURL:    srv.URL,
		DedupeWindow:  time.Hour,
		FlushInterval: time.Hour,
	})
	count := func() int {
		notifier.Flush()
		mu.Lock()
		defer mu.Unlock()
		return len(texts)
	}

	checker := NewExpiryChecker(svc, NewRepo(db), notifier, nil)

	// 1) 剩 13 天 → 落 14 天档，发一次。
	exp13 := time.Now().UTC().Add(13 * 24 * time.Hour)
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "t1"}, &exp13); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("13 天应发 1 条，实际 %d", got)
	}
	// 同一档再跑一次 → 不重复。
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatalf("Handle 2: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("同一档不应重复，实际 %d", got)
	}

	// 2) 剩 6 天 → 7 天档，再发一次。
	exp6 := time.Now().UTC().Add(6 * 24 * time.Hour)
	if err := svc.SetExpiry(ctx, "", KindAlibabaToken, &exp6, nil); err != nil {
		t.Fatalf("SetExpiry: %v", err)
	}
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("6 天应补发 7 天档，实际 %d", got)
	}

	// 3) 剩 20 小时 → 1 天档，再发一次。
	exp20h := time.Now().UTC().Add(20 * time.Hour)
	if err := svc.SetExpiry(ctx, "", KindAlibabaToken, &exp20h, nil); err != nil {
		t.Fatal(err)
	}
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 3 {
		t.Fatalf("20 小时应补发 1 天档，实际 %d", got)
	}

	// 4) 轮换（到期时间推远）→ 复位；再临近时应重新触发。
	exp30 := time.Now().UTC().Add(30 * 24 * time.Hour)
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "t2"}, &exp30); err != nil {
		t.Fatal(err)
	}
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 3 {
		t.Fatalf("推远后不应发消息，实际 %d", got)
	}
	exp10 := time.Now().UTC().Add(10 * 24 * time.Hour)
	if err := svc.SetExpiry(ctx, "", KindAlibabaToken, &exp10, nil); err != nil {
		t.Fatal(err)
	}
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 4 {
		t.Fatalf("新一轮 14 天档应重新触发，实际 %d", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(texts[0], "1688 买家 token") || !strings.Contains(texts[0], "企业级") {
		t.Fatalf("告警文案应含种类与范围: %q", texts[0])
	}
}

// 配置了 OzonRolesFetcher 后：每天刷到期时间并留 last_verified_at。
func TestExpiryRefreshFromOzon(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	shop := &Shop{ID: "s1", Name: "店一", Mode: ModeRFBS, ClientID: "c1", Currency: "CNY", Status: ShopStatusActive}
	if err := NewRepo(db).Create(ctx, shop); err != nil {
		t.Fatalf("建店: %v", err)
	}
	if _, err := svc.Put(ctx, shop.ID, KindOzonAPIKey, map[string]string{"api_key": "k"}, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 毫秒精度：MySQL DATETIME(3) 存不下纳秒，比较时先对齐（否则必然不等）。
	fakeExpiry := time.Now().UTC().Add(40 * 24 * time.Hour).Truncate(time.Millisecond)
	factory := func(_ Shop, _ *Decrypted) (OzonRolesFetcher, error) {
		return fakeFetcher{expiry: fakeExpiry}, nil
	}
	checker := NewExpiryChecker(svc, NewRepo(db), notify.New(notify.Config{}), factory)
	if err := checker.Handle(ctx, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var row Credential
	if err := db.Where("kind = ? AND store_id = ?", KindOzonAPIKey, shop.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.Equal(fakeExpiry) {
		t.Fatalf("应回写 Ozon 读到的到期时间，实际 %v", row.ExpiresAt)
	}
	if row.LastVerifiedAt == nil {
		t.Fatal("应记 last_verified_at")
	}
}

type fakeFetcher struct{ expiry time.Time }

func (f fakeFetcher) FetchExpiry(context.Context) (time.Time, error) { return f.expiry, nil }
