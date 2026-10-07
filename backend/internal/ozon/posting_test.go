package ozon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// decodeBody 在 handler 里解请求体；失败时写 500 并返回 nil。
func decodeBody(t *testing.T, w http.ResponseWriter, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("请求体不是合法 JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return nil
	}
	return body
}

// TestListPostings 拉单：请求编码（时间窗/状态/limit/with）与响应字段解析。
func TestListPostings(t *testing.T) {
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)

	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPostingList {
			t.Errorf("路径应为 %s，实际 %s", pathPostingList, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		filter, _ := body["filter"].(map[string]any)
		if filter == nil {
			t.Error("请求体缺 filter")
		} else {
			if filter["since"] != "2026-05-01T00:00:00Z" {
				t.Errorf("filter.since 不符: %v", filter["since"])
			}
			if filter["to"] != "2026-05-07T12:30:00Z" {
				t.Errorf("filter.to 不符: %v", filter["to"])
			}
			statuses, _ := filter["statuses"].([]any)
			if len(statuses) != 1 || statuses[0] != "awaiting_packaging" {
				t.Errorf("filter.statuses 不符: %v", filter["statuses"])
			}
		}
		if body["limit"] != float64(100) {
			t.Errorf("limit 应为 100，实际 %v", body["limit"])
		}
		if _, ok := body["cursor"]; ok {
			t.Error("cursor 为空时不应出现在请求里")
		}
		if _, ok := body["sort_dir"]; ok {
			t.Error("sort_dir 未指定时不应出现在请求里")
		}
		with, _ := body["with"].(map[string]any)
		if with == nil || with["analytics_data"] != true {
			t.Errorf("with 不符: %v", body["with"])
		}
		writeJSON(t, w, readTestdata(t, "listpostings_response.json"))
	}
	c := newTestClient(t, 0, h)

	page, err := c.ListPostings(context.Background(), ListPostingsParams{
		Since:    Time{since},
		To:       Time{to},
		Statuses: []string{"awaiting_packaging"},
		Limit:    100,
		With:     PostingWith{AnalyticsData: true},
	})
	if err != nil {
		t.Fatalf("ListPostings 失败: %v", err)
	}
	if len(page.Postings) == 0 {
		t.Fatal("应解析出 postings")
	}
	p := page.Postings[0]
	// S1-D 要用的字段必须解析出来（字段名错会解析成零值，这里即失败）。
	if p.PostingNumber == "" || p.OrderNumber == "" || p.Status == "" || p.Substatus == "" {
		t.Errorf("核心字段解析不全: %+v", p)
	}
	if p.TplIntegrationType == "" {
		t.Error("tpl_integration_type 未解析")
	}
	if p.ShipmentDate.IsZero() {
		t.Error("shipment_date 未解析")
	}
	if len(p.Products) == 0 || p.Products[0].Price == nil {
		t.Errorf("products/price 未解析: %+v", p.Products)
	}
	if p.AnalyticsData == nil || p.Customer == nil {
		t.Error("analytics_data / customer 未解析（with 附加数据）")
	}
}

// TestListPostings_MissingTimeWindow 缺时间窗在本地拒绝，不发请求。
func TestListPostings_MissingTimeWindow(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)

	_, err := c.ListPostings(context.Background(), ListPostingsParams{
		To: Time{time.Now()},
	})
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("缺 Since 应返回 ErrInvalidParams，实际: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("本地校验失败不应发请求，实际 %d 次", got)
	}
}

// TestListUnfulfilled 拉未完成单：cutoff 时间窗编码与分页字段解析。
func TestListUnfulfilled(t *testing.T) {
	from := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)

	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPostingUnfulfilled {
			t.Errorf("路径应为 %s，实际 %s", pathPostingUnfulfilled, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		filter, _ := body["filter"].(map[string]any)
		if filter == nil {
			t.Error("请求体缺 filter")
		} else {
			if filter["cutoff_from"] != "2026-05-01T08:00:00Z" || filter["cutoff_to"] != "2026-05-02T08:00:00Z" {
				t.Errorf("cutoff 时间窗不符: %v ~ %v", filter["cutoff_from"], filter["cutoff_to"])
			}
			if _, ok := filter["delivering_date_from"]; ok {
				t.Error("未指定 delivering_date 时不应出现")
			}
		}
		writeJSON(t, w, readTestdata(t, "listunfulfilled_response.json"))
	}
	c := newTestClient(t, 0, h)

	page, err := c.ListUnfulfilled(context.Background(), ListUnfulfilledParams{
		CutoffFrom: Time{from},
		CutoffTo:   Time{to},
	})
	if err != nil {
		t.Fatalf("ListUnfulfilled 失败: %v", err)
	}
	if len(page.Postings) == 0 {
		t.Fatal("应解析出 postings")
	}
	p := page.Postings[0]
	if p.PostingNumber == "" || p.Status == "" {
		t.Errorf("核心字段解析不全: %+v", p)
	}
	if page.Count <= 0 {
		t.Error("count 未解析")
	}
}

// TestGetPosting 单详情：请求体与 v3 result 解析（含无毫秒时间的容忍）。
func TestGetPosting(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPostingGet {
			t.Errorf("路径应为 %s，实际 %s", pathPostingGet, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		if body["posting_number"] != "0132112277-0101-1" {
			t.Errorf("posting_number 不符: %v", body["posting_number"])
		}
		writeJSON(t, w, readTestdata(t, "getposting_response.json"))
	}
	c := newTestClient(t, 0, h)

	detail, err := c.GetPosting(context.Background(), "0132112277-0101-1")
	if err != nil {
		t.Fatalf("GetPosting 失败: %v", err)
	}
	if detail.PostingNumber == "" || detail.Status == "" || detail.Substatus == "" {
		t.Errorf("核心字段解析不全: %+v", detail)
	}
	if detail.TplIntegrationType == "" {
		t.Error("tpl_integration_type 未解析")
	}
	if detail.ShipmentDate.IsZero() {
		t.Error("shipment_date 未解析（样例值无毫秒，验证时间容忍）")
	}
	if detail.PreviousSubstatus == "" {
		t.Error("previous_substatus 未解析（单详情特有字段）")
	}
	if len(detail.Products) == 0 || detail.Products[0].CurrencyCode == "" {
		t.Errorf("products 解析不全（v3 结构）: %+v", detail.Products)
	}
}

// TestGetPosting_EmptyNumber 空寄件号本地拒绝。
func TestGetPosting_EmptyNumber(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)

	if _, err := c.GetPosting(context.Background(), ""); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("空寄件号应返回 ErrInvalidParams，实际: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("不应发请求")
	}
}

// TestShipPosting 备货：请求编码（packages → products）与返回寄件号解析。
func TestShipPosting(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPostingShip {
			t.Errorf("路径应为 %s，实际 %s", pathPostingShip, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		if body["posting_number"] != "89491381-0072-1" {
			t.Errorf("posting_number 不符: %v", body["posting_number"])
		}
		packages, _ := body["packages"].([]any)
		if len(packages) != 1 {
			t.Fatalf("packages 应有 1 个，实际 %v", body["packages"])
		}
		first, _ := packages[0].(map[string]any)
		products, _ := first["products"].([]any)
		if len(products) != 1 {
			t.Fatalf("products 应有 1 个: %v", first)
		}
		prod, _ := products[0].(map[string]any)
		if prod["product_id"] != float64(185479045) || prod["quantity"] != float64(1) {
			t.Errorf("products[0] 不符: %v", prod)
		}
		writeJSON(t, w, readTestdata(t, "shipposting_response.json"))
	}
	c := newTestClient(t, 0, h)

	res, err := c.ShipPosting(context.Background(), "89491381-0072-1", []ShipPackage{
		{Products: []ShipProduct{{ProductID: 185479045, Quantity: 1}}},
	})
	if err != nil {
		t.Fatalf("ShipPosting 失败: %v", err)
	}
	if len(res.PostingNumbers) == 0 || res.PostingNumbers[0] == "" {
		t.Fatalf("应解析出 result 寄件号: %+v", res)
	}
}

// TestShipPosting_Validation 参数校验：空单号 / 空包裹 / 包裹里没商品。
func TestShipPosting_Validation(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)
	ctx := context.Background()
	valid := ShipPackage{Products: []ShipProduct{{ProductID: 1, Quantity: 1}}}

	cases := []struct {
		name     string
		number   string
		packages []ShipPackage
	}{
		{"空单号", "", []ShipPackage{valid}},
		{"空包裹列表", "A-1", nil},
		{"包裹没商品", "A-1", []ShipPackage{{}}},
	}
	for _, tc := range cases {
		if _, err := c.ShipPosting(ctx, tc.number, tc.packages); !errors.Is(err, ErrInvalidParams) {
			t.Errorf("%s: 应返回 ErrInvalidParams，实际: %v", tc.name, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("本地校验失败不应发请求，实际 %d 次", got)
	}
}
