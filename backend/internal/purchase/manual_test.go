package purchase

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func taskWithPayload(t *testing.T, pay TaskPayload) *PurchaseTask {
	t.Helper()
	raw, err := json.Marshal(pay)
	if err != nil {
		t.Fatal(err)
	}
	return &PurchaseTask{ID: "task-1", OrderID: "order-1", Payload: raw, Channel: "manual", Status: StatusPending}
}

// 验收：备料单收货地址 = 中转点、备注含 posting_number、**无买家个人信息**。
func TestBuildMaterialSheet(t *testing.T) {
	deadline := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	task := taskWithPayload(t, TaskPayload{
		PostingNumber: "0123-4567-89",
		RelayName:     "深圳货代仓",
		RelayAddress:  "广东省深圳市宝安区某路 1 号",
		RelayContact:  "张三 13800000000",
		Items: []TaskPayloadItem{
			{OzonOfferID: "SKU-A", ItemID: "6789", SkuID: "red", URL: "https://detail.1688.com/offer/6789.html", Qty: 2, UnitPrice: decimal.RequireFromString("12.34"), Currency: "CNY"},
		},
	})

	sheet, err := BuildMaterialSheet(task, &deadline)
	if err != nil {
		t.Fatalf("构造备料单失败: %v", err)
	}

	// 收货地址 = 中转点
	if sheet.Receiver != "深圳货代仓" || !strings.Contains(sheet.Address, "宝安区") {
		t.Errorf("收货地址应来自中转点，实际 %q / %q", sheet.Receiver, sheet.Address)
	}
	// 备注含 posting_number（人读版也要带）
	if !strings.Contains(sheet.Text, "0123-4567-89") {
		t.Errorf("备料单文本应含 posting_number，实际:\n%s", sheet.Text)
	}
	// 期望时效
	if sheet.Deadline == nil || !sheet.Deadline.Equal(deadline) {
		t.Errorf("期望时效未带上: %v", sheet.Deadline)
	}
	// 商品行
	if len(sheet.Items) != 1 || sheet.Items[0].ItemID != "6789" || sheet.Items[0].Qty != 2 {
		t.Errorf("商品行不对: %+v", sheet.Items)
	}
	// 备料单里不能出现买家信息字段（结构体层面就没有买家字段；文本里也不能出现）
	for _, banned := range []string{"买家", "收件人姓名", "buyer"} {
		if strings.Contains(sheet.Text, banned) {
			t.Errorf("备料单不应出现买家信息相关字样 %q:\n%s", banned, sheet.Text)
		}
	}
}

// 验收：回填格式校验生效。
func TestFillBackValidation(t *testing.T) {
	ok := FillBackRequest{
		PlatformOrderID:    "1234567890123",
		Amount:             decimal.RequireFromString("88.50"),
		DomesticTrackingNo: "SF1234567890",
		DomesticCarrier:    "顺丰",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法回填不该报错: %v", err)
	}
	// 只补国内快递号（记已付款之后的常规动作）：平台单号与实付都留空也合法。
	onlyTracking := FillBackRequest{DomesticTrackingNo: "SF1234567890"}
	if err := onlyTracking.Validate(); err != nil {
		t.Fatalf("只补快递号不该报错: %v", err)
	}
	cases := []struct {
		name string
		req  FillBackRequest
	}{
		{"平台单号太短", FillBackRequest{PlatformOrderID: "12345", Amount: decimal.NewFromInt(1), DomesticTrackingNo: "SF1234567890"}},
		{"平台单号带非法字符", FillBackRequest{PlatformOrderID: "1688-12345", Amount: decimal.NewFromInt(1), DomesticTrackingNo: "SF1234567890"}},
		// 实付为 0 = 「这次不填」（自动任务已有金额）：格式层放行，够不够用由 FillBack 判。
		{"实付为负", FillBackRequest{PlatformOrderID: "1234567890123", Amount: decimal.NewFromInt(-5), DomesticTrackingNo: "SF1234567890"}},
		{"快递号太短", FillBackRequest{PlatformOrderID: "1234567890123", Amount: decimal.NewFromInt(1), DomesticTrackingNo: "SF12"}},
		{"快递号带中文", FillBackRequest{PlatformOrderID: "1234567890123", Amount: decimal.NewFromInt(1), DomesticTrackingNo: "顺丰1234567890"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err == nil {
				t.Fatal("应报格式错误，实际通过")
			}
		})
	}
}
