//go:build smoke

// 真实店冒烟（默认不跑，-tags smoke 才编译；只调只读方法）：
//
//	OZON_CLIENT_ID=xxx OZON_API_KEY=xxx \
//	go test -tags smoke -run TestSmoke ./internal/ozon/ -v -count=1
//
// 可选：OZON_POSTING_NUMBER=xxx-xx-x 指定单号验 GetPosting；不设则从
// ListPostings 拉到的最近一单里取。
//
// 写操作（备货、面单、传单号、发运单）不在这里跑——按 spec §7 自定细节，
// 放到 S1 父 issue 的端到端联调里验。
package ozon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// smokeClient 从环境变量取凭据；没有就跳过（CI 上安静跳过）。
func smokeClient(t *testing.T) *Client {
	t.Helper()
	clientID := os.Getenv("OZON_CLIENT_ID")
	apiKey := os.Getenv("OZON_API_KEY")
	if clientID == "" || apiKey == "" {
		t.Skip("未设 OZON_CLIENT_ID / OZON_API_KEY，跳过真实店冒烟")
	}
	// 冒烟保守限速（5 req/s），重试 2 次，别把真实店的额度抽干。
	limiter := ratelimit.New(ratelimit.Config{
		Ozon: ratelimit.SubjectConfig{DefaultRPS: 5},
	}, 2, time.Second)
	return New(limiter, Options{
		ClientID: clientID,
		APIKey:   apiKey,
		Subject:  "smoke",
	})
}

// TestSmoke_GetRoles 读密钥角色与到期时间（只读）。
func TestSmoke_GetRoles(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	roles, err := c.GetRoles(ctx)
	if err != nil {
		t.Fatalf("GetRoles 失败: %v", err)
	}
	t.Logf("冒烟 OK：/v1/roles 返回 expires_at=%s，%d 个角色",
		roles.ExpiresAt.Format(time.RFC3339), len(roles.Roles))
}

// TestSmoke_ListPostings 最近 7 天拉单（只读）。
func TestSmoke_ListPostings(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	now := time.Now().UTC()
	page, err := c.ListPostings(ctx, ListPostingsParams{
		Since: Time{now.Add(-7 * 24 * time.Hour)},
		To:    Time{now},
		Limit: 50,
		With:  PostingWith{AnalyticsData: true, FinancialData: true},
	})
	if err != nil {
		t.Fatalf("ListPostings 失败: %v", err)
	}
	t.Logf("冒烟 OK：/v4/posting/fbs/list 7 天窗口拉回 %d 条（has_next=%v）",
		len(page.Postings), page.HasNext)
	for _, p := range page.Postings[:min(3, len(page.Postings))] {
		t.Logf("  样例 %s status=%s substatus=%s tpl=%s shipment_date=%s",
			p.PostingNumber, p.Status, p.Substatus, p.TplIntegrationType, p.ShipmentDate.Format(time.RFC3339))
	}
}

// TestSmoke_GetPosting 单详情（只读）。单号优先取环境变量，否则用近 7 天第一单。
func TestSmoke_GetPosting(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	number := os.Getenv("OZON_POSTING_NUMBER")
	if number == "" {
		now := time.Now().UTC()
		page, err := c.ListPostings(ctx, ListPostingsParams{
			Since: Time{now.Add(-7 * 24 * time.Hour)},
			To:    Time{now},
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("为取单号而 ListPostings 失败: %v", err)
		}
		if len(page.Postings) == 0 {
			t.Skip("近 7 天没有寄件，且未设 OZON_POSTING_NUMBER；跳过 GetPosting 冒烟")
		}
		number = page.Postings[0].PostingNumber
	}

	detail, err := c.GetPosting(ctx, number)
	if err != nil {
		t.Fatalf("GetPosting(%s) 失败: %v", number, err)
	}
	t.Logf("冒烟 OK：/v3/posting/fbs/get %s 解析出 status=%s substatus=%s tpl=%s shipment_date=%s parent=%q",
		detail.PostingNumber, detail.Status, detail.Substatus,
		detail.TplIntegrationType, detail.ShipmentDate.Format(time.RFC3339), detail.ParentPostingNumber)
}
