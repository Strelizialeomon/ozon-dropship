//go:build integration

package shipment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"

	"github.com/gin-gonic/gin"
)

// 接口清单里打包交接 + 中转点的每个接口都过一遍 handler。
func newShipEngine(t *testing.T, env *shipEnv) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(env.svc, env.relays, audit.New(env.db)).Register(r.Group("/api"))
	return r
}

func raw(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
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
	return rec
}

func scall(t *testing.T, r *gin.Engine, method, path, body string) map[string]any {
	t.Helper()
	rec := raw(t, r, method, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s HTTP %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s %s 响应不是 JSON: %s", method, path, rec.Body.String())
	}
	return resp
}

func TestRelayPointHandlers(t *testing.T) {
	env := newShipEnv(t)
	r := newShipEngine(t, env)

	// Create
	resp := scall(t, r, http.MethodPost, "/api/relay-points",
		`{"name":"新货代仓","kind":"forwarder","address":"深圳宝安","contact":"李四 13900000000"}`)
	id := resp["data"].(map[string]any)["id"].(string)
	// 类型必须是两类中转点之一
	if resp["data"].(map[string]any)["kind"] != order.RelayKindForwarder {
		t.Fatalf("kind 不对: %v", resp["data"])
	}
	bad := raw(t, r, http.MethodPost, "/api/relay-points", `{"name":"X","kind":"whatever"}`)
	if !strings.Contains(bad.Body.String(), "1001") {
		t.Fatalf("非法 kind 应 1001: %s", bad.Body.String())
	}

	// List / Get
	resp = scall(t, r, http.MethodGet, "/api/relay-points", "")
	if len(resp["data"].([]any)) < 2 {
		t.Fatalf("列表应含新老中转点: %v", resp["data"])
	}
	scall(t, r, http.MethodGet, "/api/relay-points/"+id, "")

	// Update
	resp = scall(t, r, http.MethodPut, "/api/relay-points/"+id,
		`{"name":"新货代仓2","kind":"forwarder","address":"深圳龙华","status":"disabled"}`)
	if resp["data"].(map[string]any)["name"] != "新货代仓2" {
		t.Fatalf("更新没生效: %v", resp["data"])
	}

	// Delete
	scall(t, r, http.MethodDelete, "/api/relay-points/"+id, "")
	if resp := scall(t, r, http.MethodGet, "/api/relay-points/"+id, ""); resp["code"].(float64) != 1004 {
		t.Fatalf("删后应 1004: %v", resp)
	}
}

func TestShipmentHandlers(t *testing.T) {
	env := newShipEnv(t)
	ctx := t.Context()
	r := newShipEngine(t, env)

	o := env.seedOrder(t, "sh-1", order.Tpl3PLTracking, &env.relay.ID)
	if _, err := env.orders.Advance(ctx, o.ID, order.StatusInbound); err != nil {
		t.Fatal(err)
	}

	// GET /api/shipments（打包页）
	resp := scall(t, r, http.MethodGet, "/api/shipments?relay_point_id="+env.relay.ID, "")
	data := resp["data"].(map[string]any)
	if data["total"].(float64) != 1 {
		t.Fatalf("打包页应有 1 单，实际 %v", data["total"])
	}
	if data["items"].([]any)[0].(map[string]any)["tracking_action"] != "set" {
		t.Fatalf("3pl_tracking 动作应为 set: %v", data["items"])
	}

	// POST receive（签收）
	resp = scall(t, r, http.MethodPost, "/api/shipments/"+o.ID+"/receive", "")
	if resp["code"].(float64) != 0 || env.orderStatus(t, o.ID) != order.StatusAtRelay {
		t.Fatalf("签收失败: %v", resp)
	}
	// 重复签收：幂等，不报错
	scall(t, r, http.MethodPost, "/api/shipments/"+o.ID+"/receive", "")

	// GET label（面单）
	rec := raw(t, r, http.MethodGet, "/api/shipments/"+o.ID+"/label", "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Fatalf("面单应返回 PDF: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("面单 Content-Type 不对: %s", ct)
	}

	// POST ship（备货）
	resp = scall(t, r, http.MethodPost, "/api/shipments/"+o.ID+"/ship", "")
	if resp["code"].(float64) != 0 {
		t.Fatalf("备货失败: %v", resp)
	}
	if env.orderStatus(t, o.ID) != order.StatusHandedOver {
		t.Fatalf("备货后应 handed_over，实际 %s", env.orderStatus(t, o.ID))
	}

	// POST tracking（传单号）
	resp = scall(t, r, http.MethodPost, "/api/shipments/"+o.ID+"/tracking",
		`{"tracking_no":"CDEK999888","carrier":"CDEK"}`)
	if resp["code"].(float64) != 0 {
		t.Fatalf("传单号失败: %v", resp)
	}
	if len(env.fl.trackingSent) != 1 || env.fl.trackingSent[0] != "CDEK999888" {
		t.Fatalf("单号没传给 Ozon: %v", env.fl.trackingSent)
	}

	// GET handover-export（对照表 CSV）
	rec = raw(t, r, http.MethodGet, "/api/shipments/handover-export?relay_point_id="+env.relay.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("导出对照表 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sh-1") {
		t.Fatalf("对照表应含 posting: %s", rec.Body.String())
	}
	// 缺 relay_point_id → 1001
	resp = scall(t, r, http.MethodGet, "/api/shipments/handover-export", "")
	if resp["code"].(float64) != 1001 {
		t.Fatalf("缺参数应 1001: %v", resp)
	}
}
