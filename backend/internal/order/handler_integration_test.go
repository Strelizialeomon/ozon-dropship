//go:build integration

package order

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"

	"github.com/gin-gonic/gin"
)

// 接口清单里订单侧的每个接口都过一遍 handler。
func newOrderEngine(t *testing.T, env *orderEnv) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/api")
	NewHandler(env.repo, env.svc, audit.New(env.db)).Register(group)
	NewExceptionHandler(env.exc, audit.New(env.db)).Register(group)
	return r
}

func hcall(t *testing.T, r *gin.Engine, method, path, body string) map[string]any {
	t.Helper()
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s HTTP %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s %s 响应不是 JSON: %s", method, path, rec.Body.String())
	}
	return resp
}

func TestOrderHandlers(t *testing.T) {
	env := newOrderEnv(t)
	r := newOrderEngine(t, env)
	ctx := t.Context()

	// 造两张订单。
	env.src.listed = []Posting{
		posting("h-1", OzonAwaitingPackaging, item("SKU-1", 1, "10.00")),
		posting("h-2", OzonAwaitingRegistration, item("SKU-1", 3, "5.00")),
	}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}

	// GET /api/orders
	resp := hcall(t, r, http.MethodGet, "/api/orders?store_id="+env.shop.ID, "")
	data := resp["data"].(map[string]any)
	if data["total"].(float64) != 2 {
		t.Fatalf("订单列表应有 2 条，实际 %v", data["total"])
	}

	// 带筛选（状态）
	resp = hcall(t, r, http.MethodGet, "/api/orders?status="+StatusNew, "")
	if resp["data"].(map[string]any)["total"].(float64) != 2 {
		t.Fatalf("按状态筛选不对: %v", resp["data"])
	}

	// GET /api/orders/:id（含商品行）
	o, err := env.repo.ByPosting(ctx, env.shop.ID, "h-1")
	if err != nil {
		t.Fatal(err)
	}
	resp = hcall(t, r, http.MethodGet, "/api/orders/"+o.ID, "")
	detail := resp["data"].(map[string]any)
	if _, ok := detail["order"]; !ok {
		t.Fatalf("详情应含 order: %v", detail)
	}
	if items := detail["items"].([]any); len(items) != 1 {
		t.Fatalf("详情应含 1 条商品行，实际 %d", len(items))
	}

	// 不存在的订单 → 1004
	resp = hcall(t, r, http.MethodGet, "/api/orders/does-not-exist", "")
	if resp["code"].(float64) != 1004 {
		t.Fatalf("不存在应 1004，实际 %v", resp["code"])
	}

	// POST /api/orders/batch（set_relay_point）
	resp = hcall(t, r, http.MethodPost, "/api/orders/batch",
		`{"ids":["`+o.ID+`"],"action":"set_relay_point","relay_point_id":"relay-9"}`)
	if resp["data"].(map[string]any)["succeeded"].(float64) != 1 {
		t.Fatalf("批量改中转点应成功 1 条: %v", resp["data"])
	}
	fresh, _ := env.repo.ByID(ctx, o.ID)
	if fresh.RelayPointID == nil || *fresh.RelayPointID != "relay-9" {
		t.Fatalf("中转点没写进去: %v", fresh.RelayPointID)
	}

	// POST /api/orders/batch（plan_purchase）
	env.q.reset()
	resp = hcall(t, r, http.MethodPost, "/api/orders/batch",
		`{"ids":["`+o.ID+`"],"action":"plan_purchase"}`)
	if resp["data"].(map[string]any)["succeeded"].(float64) != 1 {
		t.Fatalf("批量生成采购任务应成功: %v", resp["data"])
	}
	if !env.q.hasType(TaskTypePurchasePlan) {
		t.Fatal("批量动作应投采购计划任务")
	}
	// 批量里带不存在的 ID → 该条失败、不整批回滚
	resp = hcall(t, r, http.MethodPost, "/api/orders/batch",
		`{"ids":["nope"],"action":"plan_purchase"}`)
	failed := resp["data"].(map[string]any)["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("不存在的订单应记失败: %v", resp["data"])
	}
}

func TestExceptionHandlers(t *testing.T) {
	env := newOrderEnv(t)
	r := newOrderEngine(t, env)
	ctx := t.Context()

	o, _, err := env.repo.UpsertFromOzon(ctx, env.shop.ID, posting("exc-1", OzonAwaitingPackaging))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.exc.Raise(ctx, RefOrder, o.ID, CodeShipFailed, "备货没成"); err != nil {
		t.Fatal(err)
	}

	// GET /api/exceptions
	resp := hcall(t, r, http.MethodGet, "/api/exceptions?ref_type=order&code="+CodeShipFailed, "")
	items := resp["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("应有 1 条异常，实际 %d", len(items))
	}
	id := items[0].(map[string]any)["id"].(string)

	// POST /api/exceptions/:id/resolve
	resp = hcall(t, r, http.MethodPost, "/api/exceptions/"+id+"/resolve", `{"note":"人工重试成功"}`)
	row := resp["data"].(map[string]any)
	if row["status"] != ExceptionResolved {
		t.Fatalf("处理状态不对: %v", row)
	}
	if row["handled_by"] != "system" {
		t.Fatalf("无登录用户时处理人应为 system，实际 %v", row["handled_by"])
	}

	// 默认列表只列未处理 → 处理完就空。
	resp = hcall(t, r, http.MethodGet, "/api/exceptions", "")
	if resp["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("默认应只看未处理: %v", resp["data"])
	}
}

// 审计：批量动作与异常处理都要留痕。
func TestOrderSideAudits(t *testing.T) {
	env := newOrderEnv(t)
	r := newOrderEngine(t, env)
	ctx := t.Context()

	o, _, err := env.repo.UpsertFromOzon(ctx, env.shop.ID, posting("audit-1", OzonAwaitingPackaging))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.exc.Raise(ctx, RefOrder, o.ID, CodeRelayStalled, "停滞"); err != nil {
		t.Fatal(err)
	}
	hcall(t, r, http.MethodPost, "/api/orders/batch", `{"ids":["`+o.ID+`"],"action":"set_relay_point","relay_point_id":"r1"}`)
	rows, _, err := env.exc.List(ctx, ExceptionFilter{RefID: o.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("异常没建出来: %v", err)
	}
	hcall(t, r, http.MethodPost, "/api/exceptions/"+rows[0].ID+"/resolve", `{"note":"ok"}`)

	var n int64
	env.db.Model(&audit.AuditLog{}).Where("action IN ?", []string{"order.batch.set_relay_point", "exception.resolve"}).Count(&n)
	if n != 2 {
		t.Fatalf("批量动作与异常处理各应留 1 条审计，实际 %d", n)
	}
}
