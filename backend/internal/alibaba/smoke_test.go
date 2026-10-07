//go:build smoke

// 冒烟测试：真实 1688 账号（人工执行；下单会花真钱）。
// 默认不跑——必须先申请到「买家自用版」权限并完成授权（子 spec §4 外部前置）。
//
// 用法：
//
//	export ALIBABA_SMOKE_APP_KEY=...        # 应用 app_key
//	export ALIBABA_SMOKE_APP_SECRET=...     # 应用 app_secret
//	export ALIBABA_SMOKE_ACCESS_TOKEN=...   # 授权拿到的 access_token
//	export ALIBABA_SMOKE_REFRESH_TOKEN=...  # 可选：有它会自动续期
//
// 第一步（预览 + 下单，会花真钱，需显式开关）：
//
//	export ALIBABA_SMOKE_ALLOW_CREATE=1
//	export ALIBABA_SMOKE_ADDRESS_JSON='{"fullName":"...","mobile":"...","phone":"...","postCode":"...","provinceText":"...","cityText":"...","areaText":"...","address":"..."}'
//	export ALIBABA_SMOKE_OFFER_ID=612345678901
//	export ALIBABA_SMOKE_SPEC_ID=b266e0726506185beaf205cbae88530d
//	export ALIBABA_SMOKE_QTY=1
//	go test -tags smoke -run TestSmokePreviewAndCreateOrder -v ./internal/alibaba/
//
// 记下日志里的 orderId → 人工去 1688 付款 → 第二步：
//
//	export ALIBABA_SMOKE_ORDER_ID=<上一步的 orderId>
//	go test -tags smoke -run TestSmokePaidOrderAndLogistics -v ./internal/alibaba/
package alibaba

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
)

// envCreds 冒烟用的凭据来源：全部从环境变量读，不落库（续期结果只留进程内）。
type envCreds struct{}

func (envCreds) Get(_ context.Context, storeID, kind string) (map[string]string, *time.Time, error) {
	if storeID != "" {
		return nil, nil, errors.New("冒烟只用企业级凭据")
	}
	switch kind {
	case kindAlibabaApp:
		return map[string]string{
			"app_key":    os.Getenv("ALIBABA_SMOKE_APP_KEY"),
			"app_secret": os.Getenv("ALIBABA_SMOKE_APP_SECRET"),
		}, nil, nil
	case kindAlibabaToken:
		p := map[string]string{"access_token": os.Getenv("ALIBABA_SMOKE_ACCESS_TOKEN")}
		if rt := os.Getenv("ALIBABA_SMOKE_REFRESH_TOKEN"); rt != "" {
			p["refresh_token"] = rt
		}
		return p, nil, nil
	}
	return nil, nil, errors.New("未知凭据种类: " + kind)
}

func (envCreds) Put(_ context.Context, _ string, _ string, _ map[string]string, _ *time.Time) error {
	return nil // 冒烟不落库（生产经 store.CredentialService；此处只验证续期与调用链路）
}

func (envCreds) SetExpiry(_ context.Context, _, _ string, _, _ *time.Time) error { return nil }

func smokeClient(t *testing.T) *Client {
	t.Helper()
	for _, k := range []string{"ALIBABA_SMOKE_APP_KEY", "ALIBABA_SMOKE_APP_SECRET", "ALIBABA_SMOKE_ACCESS_TOKEN"} {
		if os.Getenv(k) == "" {
			t.Skipf("缺少环境变量 %s，跳过冒烟（用法见本文件头部注释）", k)
		}
	}
	client, err := NewClient(Config{
		Credentials: envCreds{},
		// 真实网关；保守 1 rps 起步（限额官方未公开，实测时逐步压）。
		Registry: ratelimit.New(ratelimit.Config{
			Alibaba: ratelimit.SubjectConfig{DefaultRPS: 1},
		}, 2, 2*time.Second),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func smokeAddress(t *testing.T) Address {
	t.Helper()
	raw := os.Getenv("ALIBABA_SMOKE_ADDRESS_JSON")
	if raw == "" {
		t.Skip("缺少 ALIBABA_SMOKE_ADDRESS_JSON（中转点地址）")
	}
	var addr Address
	if err := json.Unmarshal([]byte(raw), &addr); err != nil {
		t.Fatalf("解析 ALIBABA_SMOKE_ADDRESS_JSON: %v", err)
	}
	return addr
}

func smokeCargo(t *testing.T) []Cargo {
	t.Helper()
	offerID, err := strconv.ParseUint(os.Getenv("ALIBABA_SMOKE_OFFER_ID"), 10, 64)
	if err != nil || offerID == 0 {
		t.Skip("缺少 ALIBABA_SMOKE_OFFER_ID")
	}
	// 冒烟只买 1 件低价商品（会花真钱，数量写死不从环境变量放大）。
	return []Cargo{{OfferID: offerID, SpecID: os.Getenv("ALIBABA_SMOKE_SPEC_ID"), Quantity: 1}}
}

// TestSmokePreviewAndCreateOrder 预览 + 真实下单（人工付款前一步）。
func TestSmokePreviewAndCreateOrder(t *testing.T) {
	client := smokeClient(t)
	addr := smokeAddress(t)
	cargo := smokeCargo(t)
	ctx := context.Background()

	preview, err := client.PreviewOrder(ctx, PreviewOrderRequest{Address: addr, CargoList: cargo})
	if err != nil {
		t.Fatalf("PreviewOrder: %v", err)
	}
	for i, p := range preview.OrderPreviews {
		t.Logf("预览[%d] 可下单=%v 应付(分)=%d 运费(分)=%d 通道=%s 说明=%s",
			i, p.Status, p.SumPayment, p.SumCarriage, p.FlowFlag, p.Message)
	}

	if os.Getenv("ALIBABA_SMOKE_ALLOW_CREATE") != "1" {
		t.Skip("预览已通过；真实下单会花真钱，要跑请设 ALIBABA_SMOKE_ALLOW_CREATE=1")
	}
	res, err := client.CreateOrder(ctx, CreateOrderRequest{
		Address:   addr,
		CargoList: cargo,
		Message:   "OZON-SMOKE", // 真实流程这里写 posting_number
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	t.Logf("下单成功：orderId=%s 金额(分)=%d 运费(分)=%d", res.OrderID, res.TotalSuccessAmount, res.PostFee)
	t.Logf("下一步：人工去 1688 付款，然后设 ALIBABA_SMOKE_ORDER_ID=%s 跑 TestSmokePaidOrderAndLogistics", res.OrderID)
}

// TestSmokePaidOrderAndLogistics 人工付款后：查订单与物流（验收第 5 条的冒烟部分）。
func TestSmokePaidOrderAndLogistics(t *testing.T) {
	client := smokeClient(t)
	orderIDStr := os.Getenv("ALIBABA_SMOKE_ORDER_ID")
	if orderIDStr == "" {
		t.Skip("缺少 ALIBABA_SMOKE_ORDER_ID（见 TestSmokePreviewAndCreateOrder 的提示）")
	}
	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		t.Fatalf("ALIBABA_SMOKE_ORDER_ID 不是数字: %v", err)
	}
	ctx := context.Background()

	order, err := client.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	t.Logf("订单 %s：状态=%s 应付款(元)=%s 商品=%d 条",
		order.BaseInfo.IDOfStr, order.BaseInfo.Status, order.BaseInfo.TotalAmount, len(order.ProductItems))

	lg, err := client.GetLogistics(ctx, orderID)
	if err != nil {
		t.Fatalf("GetLogistics: %v", err)
	}
	for _, info := range lg.Infos {
		t.Logf("物流：id=%s 单号=%s 状态=%s 公司=%s", info.LogisticsID, info.LogisticsBillNo, info.Status, info.LogisticsCompanyName)
	}
	for _, tr := range lg.Traces {
		t.Logf("轨迹：单号=%s 共 %d 步", tr.LogisticsBillNo, len(tr.Steps))
	}
	if len(lg.Infos) == 0 {
		t.Log("还没有物流单（卖家未发货属正常；过段时间再查）")
	}
}
