//go:build integration

package purchase

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"

	"github.com/gin-gonic/gin"
)

// 接口清单里采购任务台的每个接口都过一遍 handler。
func newPurchaseEngine(t *testing.T, env *purchaseEnv) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(env.repo, env.svc, audit.New(env.db)).Register(r.Group("/api"))
	return r
}

func pcall(t *testing.T, r *gin.Engine, method, path, body string) map[string]any {
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

func TestPurchaseTaskHandlers(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := t.Context()
	r := newPurchaseEngine(t, env)

	// 自动任务（1688 自用版）。
	o := env.seedOrder(t, "h-auto-1", "SKU-A", true)
	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := env.repo.TasksByOrder(ctx, o.ID)
	autoTaskID := tasks[0].ID

	// GET /api/purchase-tasks（带订单摘要与筛选）
	resp := pcall(t, r, http.MethodGet, "/api/purchase-tasks?store_id="+env.shop.ID, "")
	data := resp["data"].(map[string]any)
	if data["total"].(float64) != 1 {
		t.Fatalf("应有 1 个任务，实际 %v", data["total"])
	}
	item := data["items"].([]any)[0].(map[string]any)
	if item["posting_number"] != "h-auto-1" || item["executor_type"] != ExecutorAuto {
		t.Fatalf("任务台列表项不对: %v", item)
	}

	// GET /api/purchase-tasks/:id
	resp = pcall(t, r, http.MethodGet, "/api/purchase-tasks/"+autoTaskID, "")
	detail := resp["data"].(map[string]any)
	if detail["task"] == nil || detail["material_sheet"] == nil {
		t.Fatalf("详情应含任务与备料单: %v", detail)
	}
	// 不存在的任务 → 1004
	resp = pcall(t, r, http.MethodGet, "/api/purchase-tasks/nope", "")
	if resp["code"].(float64) != 1004 {
		t.Fatalf("不存在应 1004: %v", resp)
	}

	// GET material-sheet（含 posting_number、收货地址 = 中转点、无买家信息）
	resp = pcall(t, r, http.MethodGet, "/api/purchase-tasks/"+autoTaskID+"/material-sheet", "")
	sheet := resp["data"].(map[string]any)
	if sheet["posting_number"] != "h-auto-1" {
		t.Fatalf("备料单应含 posting_number: %v", sheet)
	}
	if sheet["address"] != env.relay.Address {
		t.Fatalf("备料单收货地址应 = 中转点地址，实际 %v", sheet["address"])
	}

	// POST execute（自动任务 → 投递执行）
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+autoTaskID+"/execute", "")
	if resp["code"].(float64) != 0 {
		t.Fatalf("投递执行应成功: %v", resp)
	}
	// 真把任务跑完（模拟 worker 消费），再走记已付款。
	if err := env.svc.Execute(ctx, autoTaskID); err != nil {
		t.Fatal(err)
	}
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+autoTaskID+"/mark-paid", `{"amount":"12.34"}`)
	if resp["code"].(float64) != 0 {
		t.Fatalf("记已付款应成功: %v", resp)
	}
	if status := resp["data"].(map[string]any)["status"]; status != StatusPaid {
		t.Fatalf("记已付款后应为 paid，实际 %v", status)
	}

	// 人工任务：转人工 → 备料单 → 回填
	o2 := env.seedOrder(t, "h-manual-1", "SKU-M", false)
	env.mapOffer(t, "SKU-M", "1688", "self_use")
	if err := env.svc.PlanOrder(ctx, o2.ID); err != nil {
		t.Fatal(err)
	}
	tasks2, _ := env.repo.TasksByOrder(ctx, o2.ID)
	manualTaskID := tasks2[0].ID

	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+manualTaskID+"/convert-manual", `{"assignee":"小王","note":"这单人工下"}`)
	if resp["code"].(float64) != 0 {
		t.Fatalf("转人工应成功: %v", resp)
	}
	if resp["data"].(map[string]any)["executor_type"] != ExecutorManual {
		t.Fatalf("转人工后执行器应为 manual: %v", resp["data"])
	}
	// 人工任务不能自动执行 → 1001
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+manualTaskID+"/execute", "")
	if resp["code"].(float64) != 1001 {
		t.Fatalf("人工任务自动执行应被拒: %v", resp)
	}

	// 回填（格式不对先拦）
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+manualTaskID+"/fill-back",
		`{"platform_order_id":"1234567890123","amount":"6.5","domestic_tracking_no":"SF1"}`)
	if resp["code"].(float64) != 1001 {
		t.Fatalf("快递号格式不对应 1001: %v", resp)
	}
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+manualTaskID+"/fill-back",
		`{"platform_order_id":"1234567890123","amount":"6.5","domestic_carrier":"顺丰","domestic_tracking_no":"SF1234567890"}`)
	if resp["code"].(float64) != 0 {
		t.Fatalf("回填应成功: %v", resp)
	}
	if status := resp["data"].(map[string]any)["status"]; status != StatusShipped {
		t.Fatalf("回填后应为 shipped，实际 %v", status)
	}
	// 备料单文本带 posting_number（人工照着下单用）
	resp = pcall(t, r, http.MethodGet, "/api/purchase-tasks/"+manualTaskID+"/material-sheet", "")
	if !strings.Contains(resp["data"].(map[string]any)["text"].(string), "h-manual-1") {
		t.Fatalf("备料单文本应含 posting_number: %v", resp["data"])
	}

	// 已回填的任务再回填 → 拦（不允许倒回）
	resp = pcall(t, r, http.MethodPost, "/api/purchase-tasks/"+manualTaskID+"/fill-back",
		`{"platform_order_id":"1234567890123","amount":"6.5","domestic_tracking_no":"SF1234567890"}`)
	if resp["code"].(float64) == 0 {
		t.Fatalf("已完成任务不该能再回填: %v", resp)
	}
}
