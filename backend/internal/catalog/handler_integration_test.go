//go:build integration

package catalog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/gin-gonic/gin"
)

// 接口清单里的每个接口都过一遍 handler（映射与报价页的 9 个端点）。
func newCatalogEngine(t *testing.T) (*gin.Engine, *Repo) {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake: %v", err)
	}
	gin.SetMode(gin.TestMode)
	db := testutil.MySQL(t)
	testutil.Truncate(t, db)
	repo := NewRepo(db)
	h := NewHandler(repo, audit.New(db))
	r := gin.New()
	h.Register(r.Group("/api"))
	return r, repo
}

func call(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func mustOK(t *testing.T, rec *httptest.ResponseRecorder, what string) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s HTTP %d: %s", what, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s 响应不是 JSON: %s", what, rec.Body.String())
	}
	if code, _ := resp["code"].(float64); code != 0 {
		t.Fatalf("%s 业务失败: %s", what, rec.Body.String())
	}
	return resp
}

func TestSupplierOfferCRUD(t *testing.T) {
	r, _ := newCatalogEngine(t)

	// Create
	rec := mustOK(t, call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"1688","item_id":"6789","sku_id":"red","purchase_price":"8.5","order_channel":"self_use"}`), "建货源")
	data := rec["data"].(map[string]any)
	id := data["id"].(string)
	if data["currency"] != "CNY" || data["status"] != "active" {
		t.Fatalf("默认值不对: %v", data)
	}

	// List
	rec = mustOK(t, call(t, r, http.MethodGet, "/api/supplier-offers?platform=1688", ""), "列货源")
	if items := rec["data"].([]any); len(items) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(items))
	}

	// Get
	mustOK(t, call(t, r, http.MethodGet, "/api/supplier-offers/"+id, ""), "取货源")

	// Update
	rec = mustOK(t, call(t, r, http.MethodPut, "/api/supplier-offers/"+id,
		`{"platform":"1688","item_id":"6789","sku_id":"blue","purchase_price":"9.9","order_channel":"self_use"}`), "改货源")
	if rec["data"].(map[string]any)["sku_id"] != "blue" {
		t.Fatalf("更新没生效: %v", rec["data"])
	}

	// Delete
	mustOK(t, call(t, r, http.MethodDelete, "/api/supplier-offers/"+id, ""), "删货源")
	var resp map[string]any
	_ = json.Unmarshal(call(t, r, http.MethodGet, "/api/supplier-offers/"+id, "").Body.Bytes(), &resp)
	if code, _ := resp["code"].(float64); code != 1004 {
		t.Fatalf("删后应 1004，实际 %v", resp)
	}
}

// S1 的开单通道限制：跨境自用版 S2 才接；非 1688 货源只能走人工通道。
func TestSupplierOfferChannelValidation(t *testing.T) {
	r, _ := newCatalogEngine(t)

	rec := call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"1688","item_id":"1","order_channel":"cross_border"}`)
	if !strings.Contains(rec.Body.String(), "cross_border") || !strings.Contains(rec.Body.String(), "1001") {
		t.Fatalf("跨境自用版通道应被拒（S2 才接）: %s", rec.Body.String())
	}
	rec = call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"pdd","item_id":"1","order_channel":"self_use"}`)
	if !strings.Contains(rec.Body.String(), "1001") {
		t.Fatalf("拼多多货源只能走人工通道: %s", rec.Body.String())
	}
	mustOK(t, call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"pdd","item_id":"1","order_channel":"manual"}`), "拼多多人工货源")
}

func TestOfferLinkCRUD(t *testing.T) {
	r, _ := newCatalogEngine(t)
	rec := mustOK(t, call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"1688","item_id":"6789","order_channel":"self_use"}`), "建货源")
	offerID := rec["data"].(map[string]any)["id"].(string)

	// Create
	rec = mustOK(t, call(t, r, http.MethodPost, "/api/offer-links",
		`{"store_id":"store-1","ozon_offer_id":"SKU-A","supplier_offer_id":"`+offerID+`"}`), "建映射")
	linkID := rec["data"].(map[string]any)["id"].(string)

	// 重复三元组 → 1002
	dup := call(t, r, http.MethodPost, "/api/offer-links",
		`{"store_id":"store-1","ozon_offer_id":"SKU-A","supplier_offer_id":"`+offerID+`"}`)
	if !strings.Contains(dup.Body.String(), "1002") {
		t.Fatalf("重复映射应 1002: %s", dup.Body.String())
	}

	// List
	rec = mustOK(t, call(t, r, http.MethodGet, "/api/offer-links?store_id=store-1", ""), "列映射")
	if items := rec["data"].([]any); len(items) != 1 {
		t.Fatalf("应有 1 条映射，实际 %d", len(items))
	}

	// Update（priority / target_stock）
	rec = mustOK(t, call(t, r, http.MethodPut, "/api/offer-links/"+linkID,
		`{"priority":2,"target_stock":50}`), "改映射")
	if rec["data"].(map[string]any)["priority"].(float64) != 2 {
		t.Fatalf("priority 没更新: %v", rec["data"])
	}

	// Delete
	mustOK(t, call(t, r, http.MethodDelete, "/api/offer-links/"+linkID, ""), "删映射")
}

// ResolvePrimary：按 priority 取主货源，跳过失效商品。
func TestResolvePrimary(t *testing.T) {
	r, repo := newCatalogEngine(t)
	ctx := t.Context()

	rec := mustOK(t, call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"1688","item_id":"A","order_channel":"self_use"}`), "建主货源")
	mainID := rec["data"].(map[string]any)["id"].(string)
	rec = mustOK(t, call(t, r, http.MethodPost, "/api/supplier-offers",
		`{"platform":"1688","item_id":"B","order_channel":"self_use"}`), "建备货源")
	backupID := rec["data"].(map[string]any)["id"].(string)

	mustOK(t, call(t, r, http.MethodPost, "/api/offer-links",
		`{"store_id":"s1","ozon_offer_id":"SKU-A","supplier_offer_id":"`+mainID+`","priority":1}`), "主映射")
	mustOK(t, call(t, r, http.MethodPost, "/api/offer-links",
		`{"store_id":"s1","ozon_offer_id":"SKU-A","supplier_offer_id":"`+backupID+`","priority":2}`), "备映射")

	link, offer, err := repo.ResolvePrimary(ctx, "s1", "SKU-A")
	if err != nil {
		t.Fatal(err)
	}
	if offer.ID != mainID || link.Priority != 1 {
		t.Fatalf("应取 priority=1 的主货源，实际 %+v / %+v", link, offer)
	}

	// 主货源标记失效 → 落到备货源。
	if err := repo.SoftDeleteOffer(ctx, mainID); err != nil {
		t.Fatal(err)
	}
	_, offer2, err := repo.ResolvePrimary(ctx, "s1", "SKU-A")
	if err != nil || offer2.ID != backupID {
		t.Fatalf("主货源删掉后应落到备货源，实际 %+v（err=%v）", offer2, err)
	}

	// 没有映射 → 明确报错。
	if _, _, err := repo.ResolvePrimary(ctx, "s1", "SKU-NONE"); err == nil {
		t.Fatal("没有映射应报错")
	}
}
