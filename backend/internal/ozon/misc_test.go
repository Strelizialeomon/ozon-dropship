package ozon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
)

// TestGetPackageLabel 面单：请求体是多寄件号数组，响应是二进制原样返回。
func TestGetPackageLabel(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake label bytes")
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathPostingLabel {
			t.Errorf("路径应为 %s，实际 %s", pathPostingLabel, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		nums, _ := body["posting_number"].([]any)
		if len(nums) != 2 || nums[0] != "A-1" || nums[1] != "B-2" {
			t.Errorf("posting_number 数组不符: %v", body["posting_number"])
		}
		w.Header().Set("Content-Type", "application/pdf")
		if _, err := w.Write(pdf); err != nil {
			t.Errorf("写响应失败: %v", err)
		}
	}
	c := newTestClient(t, 0, h)

	got, err := c.GetPackageLabel(context.Background(), []string{"A-1", "B-2"})
	if err != nil {
		t.Fatalf("GetPackageLabel 失败: %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("应原样返回 PDF 字节: %q", got)
	}
}

// TestGetPackageLabel_Validation 空列表 / 超 20 个本地拒绝。
func TestGetPackageLabel_Validation(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)
	ctx := context.Background()

	if _, err := c.GetPackageLabel(ctx, nil); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("空列表应返回 ErrInvalidParams，实际: %v", err)
	}
	tooMany := make([]string, maxLabelPostings+1)
	for i := range tooMany {
		tooMany[i] = "P-" + strconv.Itoa(i)
	}
	if _, err := c.GetPackageLabel(ctx, tooMany); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("超过 %d 个应返回 ErrInvalidParams，实际: %v", maxLabelPostings, err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("不应发请求")
	}
}

// TestCreatePackageLabel 面单两步流程·创建任务：请求编码与任务解析。
func TestCreatePackageLabel(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathLabelTaskCreate {
			t.Errorf("路径应为 %s，实际 %s", pathLabelTaskCreate, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		nums, _ := body["posting_numbers"].([]any)
		if len(nums) != 1 || nums[0] != "48173252-0033-2" {
			t.Errorf("posting_numbers 不符: %v", body["posting_numbers"])
		}
		writeJSON(t, w, readTestdata(t, "labeltaskcreate_response.json"))
	}
	c := newTestClient(t, 0, h)

	tasks, err := c.CreatePackageLabel(context.Background(), []string{"48173252-0033-2"})
	if err != nil {
		t.Fatalf("CreatePackageLabel 失败: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID == 0 || tasks[0].TaskType == "" {
		t.Fatalf("任务解析不符: %+v", tasks)
	}
}

// TestCreatePackageLabel_Validation 空列表本地拒绝。
func TestCreatePackageLabel_Validation(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)

	if _, err := c.CreatePackageLabel(context.Background(), nil); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("空列表应返回 ErrInvalidParams，实际: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("不应发请求")
	}
}

// TestGetPackageLabelTask 面单两步流程·查任务：请求编码与结果解析。
func TestGetPackageLabelTask(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathLabelTaskGet {
			t.Errorf("路径应为 %s，实际 %s", pathLabelTaskGet, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		if body["task_id"] != float64(123456789) {
			t.Errorf("task_id 不符: %v", body["task_id"])
		}
		writeJSON(t, w, readTestdata(t, "labeltaskget_response.json"))
	}
	c := newTestClient(t, 0, h)

	res, err := c.GetPackageLabelTask(context.Background(), 123456789)
	if err != nil {
		t.Fatalf("GetPackageLabelTask 失败: %v", err)
	}
	if res.FileURL == "" {
		t.Error("file_url 未解析")
	}
	if res.Status == nil || res.Status.Code != LabelTaskCompleted {
		t.Errorf("status 未解析: %+v", res.Status)
	}
	if len(res.Status.UnprintedPostings) != 1 || res.Status.UnprintedPostings[0].PostingNumber == "" {
		t.Errorf("unprinted_postings 未解析: %+v", res.Status)
	}
}

// TestGetPackageLabelTask_Validation 非法 taskID 本地拒绝。
func TestGetPackageLabelTask_Validation(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)

	if _, err := c.GetPackageLabelTask(context.Background(), 0); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("taskID=0 应返回 ErrInvalidParams，实际: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("不应发请求")
	}
}

// TestSetTrackingNumber 传单号：请求编码与逐条结果解析。
func TestSetTrackingNumber(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathTrackingNumberSet {
			t.Errorf("路径应为 %s，实际 %s", pathTrackingNumberSet, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		items, _ := body["tracking_numbers"].([]any)
		if len(items) != 1 {
			t.Fatalf("tracking_numbers 应有 1 条: %v", body["tracking_numbers"])
		}
		item, _ := items[0].(map[string]any)
		if item["posting_number"] != "48173252-0033-2" || item["tracking_number"] != "TRK-777" {
			t.Errorf("tracking_numbers[0] 不符: %v", item)
		}
		writeJSON(t, w, readTestdata(t, "settrackingnumber_response.json"))
	}
	c := newTestClient(t, 0, h)

	results, err := c.SetTrackingNumber(context.Background(), []TrackingNumber{
		{PostingNumber: "48173252-0033-2", TrackingNumber: "TRK-777"},
	})
	if err != nil {
		t.Fatalf("SetTrackingNumber 失败: %v", err)
	}
	if len(results) != 1 || !results[0].OK || results[0].PostingNumber == "" {
		t.Fatalf("逐条结果解析不符: %+v", results)
	}
}

// TestSetTrackingNumber_Validation 缺单号 / 缺运单号本地拒绝。
func TestSetTrackingNumber_Validation(t *testing.T) {
	var calls int32
	h := func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }
	c := newTestClient(t, 0, h)
	ctx := context.Background()

	if _, err := c.SetTrackingNumber(ctx, nil); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("空列表应拒绝，实际: %v", err)
	}
	if _, err := c.SetTrackingNumber(ctx, []TrackingNumber{{PostingNumber: "A-1"}}); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("缺运单号应拒绝，实际: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("本地校验失败不应发请求")
	}
}

// TestGetRoles 密钥角色：expires_at 与 roles 解析。
func TestGetRoles(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathRoles {
			t.Errorf("路径应为 %s，实际 %s", pathRoles, r.URL.Path)
		}
		writeJSON(t, w, readTestdata(t, "getroles_response.json"))
	}
	c := newTestClient(t, 0, h)

	roles, err := c.GetRoles(context.Background())
	if err != nil {
		t.Fatalf("GetRoles 失败: %v", err)
	}
	if roles.ExpiresAt.IsZero() {
		t.Error("expires_at 未解析（S1-A 到期检查要用）")
	}
	if len(roles.Roles) == 0 || roles.Roles[0].Name == "" {
		t.Errorf("roles 未解析: %+v", roles.Roles)
	}
}

// TestListDeliveryMethods 物流方式：请求 filter 编码与响应解析。
func TestListDeliveryMethods(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathDeliveryMethodList {
			t.Errorf("路径应为 %s，实际 %s", pathDeliveryMethodList, r.URL.Path)
		}
		body := decodeBody(t, w, r)
		if body == nil {
			return
		}
		filter, _ := body["filter"].(map[string]any)
		statuses, _ := filter["status"].([]any)
		if len(statuses) != 1 || statuses[0] != DeliveryMethodActive {
			t.Errorf("filter.status 不符: %v", filter["status"])
		}
		writeJSON(t, w, readTestdata(t, "listdeliverymethods_response.json"))
	}
	c := newTestClient(t, 0, h)

	page, err := c.ListDeliveryMethods(context.Background(), ListDeliveryMethodsParams{
		Statuses: []string{DeliveryMethodActive},
	})
	if err != nil {
		t.Fatalf("ListDeliveryMethods 失败: %v", err)
	}
	if len(page.DeliveryMethods) != 2 {
		t.Fatalf("应有 2 个配送方式，实际 %d", len(page.DeliveryMethods))
	}
	first := page.DeliveryMethods[0]
	if first.ID == 0 || first.Name == "" || first.Status != DeliveryMethodActive {
		t.Errorf("字段解析不符: %+v", first)
	}
	if first.TplIntegrationType == "" {
		t.Error("tpl_integration_type 未解析")
	}
	if first.DropOffPoint == nil || first.DropOffPoint.Coordinates == nil {
		t.Errorf("tpl_dropoff_point 嵌套结构未解析: %+v", first.DropOffPoint)
	}
	if page.DeliveryMethods[1].TplIntegrationType != TplIntegrationOzon {
		t.Errorf("第二条 tpl_integration_type 应解析为 ozon: %+v", page.DeliveryMethods[1])
	}
	if page.HasNext {
		t.Error("has_next 应为 false")
	}
}
