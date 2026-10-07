//go:build integration

package shipment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/catalog"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/purchase"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/hibiken/asynq"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// fakeFulfiller 可编程的 Ozon 履约假实现（S1-B 未合并时的替身）。
type fakeFulfiller struct {
	shipErr      error
	substatus    string
	shipCalls    int
	trackingSent []string
	label        []byte
}

func (f *fakeFulfiller) ShipPosting(context.Context, string) error {
	f.shipCalls++
	return f.shipErr
}

func (f *fakeFulfiller) GetPosting(context.Context, string) (*PostingState, error) {
	return &PostingState{Status: "awaiting_deliver", Substatus: f.substatus}, nil
}

func (f *fakeFulfiller) GetPackageLabel(context.Context, string) ([]byte, error) {
	if f.label == nil {
		return []byte("%PDF-1.4 fake"), nil
	}
	return f.label, nil
}

func (f *fakeFulfiller) SetTrackingNumber(_ context.Context, _, trackingNo, _ string) error {
	f.trackingSent = append(f.trackingSent, trackingNo)
	return nil
}

type fakeEnqueuer struct {
	mu    sync.Mutex
	tasks []queue.Task
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, t queue.Task, _ ...asynq.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, t)
	return nil
}

func (f *fakeEnqueuer) has(taskType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.Type == taskType {
			return true
		}
	}
	return false
}

type shipEnv struct {
	db        *gorm.DB
	svc       *Service
	repo      *Repo
	orders    *order.Service
	orderRepo *order.Repo
	relays    *order.RelayRepo
	shop      *store.Shop
	relay     *order.RelayPoint
	fl        *fakeFulfiller
	q         *fakeEnqueuer
}

func newShipEnv(t *testing.T) *shipEnv {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake: %v", err)
	}
	db := testutil.MySQL(t)
	testutil.Truncate(t, db)
	v := testutil.Vault(t)
	rec := audit.New(db)
	ctx := context.Background()

	shops := store.NewRepo(db)
	creds := store.NewCredentialService(db, v, rec)
	shop := &store.Shop{
		ID: snowflake.GenStringID(), Name: "发货测试店", Mode: store.ModeRFBS,
		ClientID: "c1", Currency: "CNY", Status: store.ShopStatusActive,
	}
	if err := shops.Create(ctx, shop); err != nil {
		t.Fatalf("建店: %v", err)
	}
	if _, err := creds.Put(ctx, shop.ID, store.KindOzonAPIKey, map[string]string{"api_key": "k-12345678"}, nil); err != nil {
		t.Fatalf("建凭据: %v", err)
	}
	relays := order.NewRelayRepo(db)
	relay := &order.RelayPoint{
		ID: snowflake.GenStringID(), Name: "自有仓", Kind: order.RelayKindOwnWarehouse,
		Address: "深圳市宝安区仓", Status: order.RelayStatusActive,
	}
	if err := relays.Create(ctx, relay); err != nil {
		t.Fatalf("建中转点: %v", err)
	}

	fl := &fakeFulfiller{}
	que := &fakeEnqueuer{}
	orderRepo := order.NewRepo(db)
	orderSvc := order.NewService(orderRepo, order.NewExceptions(db), shops, creds, nil, que)
	purchaseRepo := purchase.NewRepo(db)
	svc := NewService(NewRepo(db), orderSvc, purchaseRepo, creds,
		func(store.Shop, *store.Decrypted) (PostingFulfiller, error) { return fl, nil }, rec)

	return &shipEnv{
		db: db, svc: svc, repo: NewRepo(db), orders: orderSvc, orderRepo: orderRepo,
		relays: relays, shop: shop, relay: relay, fl: fl, q: que,
	}
}

// seedOrder 造一张订单：tpl 决定传单号动作；relay 决定挂哪个中转点。
// Ozon 状态用 awaiting_packaging（映射不驱动内部状态，订单落 new）——
// 用 awaiting_deliver 会让建单直接跳到 handed_over，后面的推进就测不出来了。
func (e *shipEnv) seedOrder(t *testing.T, postingNumber, tpl string, relayPointID *string) *order.Order {
	t.Helper()
	ctx := context.Background()
	p := order.Posting{
		PostingNumber:      postingNumber,
		OrderNumber:        "ORD-" + postingNumber,
		Status:             order.OzonAwaitingPackaging,
		TplIntegrationType: tpl,
		Currency:           "CNY",
		Items:              []order.PostingItem{{OzonOfferID: "SKU-A", Qty: 1, Price: decimal.RequireFromString("10.00"), Currency: "CNY"}},
	}
	o, _, err := e.orderRepo.UpsertFromOzon(ctx, e.shop.ID, p)
	if err != nil {
		t.Fatalf("造订单: %v", err)
	}
	if err := e.orderRepo.UpsertItems(ctx, o.ID, p.Items); err != nil {
		t.Fatalf("造订单行: %v", err)
	}
	if relayPointID != nil {
		if _, err := e.orderRepo.SetRelayPoint(ctx, o.ID, *relayPointID); err != nil {
			t.Fatalf("挂中转点: %v", err)
		}
	}
	return o
}

func (e *shipEnv) orderStatus(t *testing.T, orderID string) string {
	t.Helper()
	o, err := e.orderRepo.ByID(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	return o.Status
}

func (e *shipEnv) openExceptionCodes(t *testing.T, orderID string) []string {
	t.Helper()
	rows, _, err := e.orders.Exceptions().List(context.Background(), order.ExceptionFilter{RefID: orderID})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Code)
	}
	return out
}

// 验收：两类中转点的状态推进（签收 → at_relay；备货 → handed_over）。
func TestReceiveAndShipAdvanceStatus(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()

	for _, kind := range []string{order.RelayKindForwarder, order.RelayKindOwnWarehouse} {
		t.Run(kind, func(t *testing.T) {
			relay := &order.RelayPoint{
				ID: snowflake.GenStringID(), Name: "中转点-" + kind, Kind: kind,
				Address: "深圳某仓", Status: order.RelayStatusActive,
			}
			if err := env.relays.Create(ctx, relay); err != nil {
				t.Fatal(err)
			}
			o := env.seedOrder(t, "ship-"+kind, order.TplOzon, &relay.ID)

			// 签收前备货：应被拦住（默认货到中转点才备货）。
			if _, err := env.svc.Ship(ctx, o.ID, "", ""); err == nil {
				t.Fatal("货物还没到中转点，不该能备货")
			}
			// 签收。
			if _, err := env.svc.Receive(ctx, o.ID); err != nil {
				t.Fatalf("签收失败: %v", err)
			}
			if got := env.orderStatus(t, o.ID); got != order.StatusAtRelay {
				t.Fatalf("签收后应为 at_relay，实际 %s", got)
			}
			if !env.q.has(order.TaskTypePurchaseClose) {
				t.Error("签收后应投「关采购任务」")
			}
			// 备货（复核 substatus 正常）→ handed_over。
			res, err := env.svc.Ship(ctx, o.ID, "", "")
			if err != nil {
				t.Fatalf("备货失败: %v", err)
			}
			if got := env.orderStatus(t, o.ID); got != order.StatusHandedOver {
				t.Fatalf("备货后应为 handed_over，实际 %s", got)
			}
			if res.TrackingAction != "none" {
				t.Errorf("tpl=ozon 的传单号动作应为 none，实际 %s", res.TrackingAction)
			}
			sh, err := env.repo.ByOrder(ctx, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sh.HandedOverAt == nil {
				t.Error("发运记录应记 handed_over_at")
			}
			if sh.TrackingSource == nil || *sh.TrackingSource != TrackingSourceOzon {
				t.Errorf("tpl=ozon 应记 tracking_source=ozon，实际 %v", sh.TrackingSource)
			}
		})
	}
}

// 验收：备货返回成功但 substatus = ship_failed 时进异常池，且不推进状态。
func TestShipChecksSubstatusShipFailed(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "shipfail-1", order.TplOzon, &env.relay.ID)

	env.fl.substatus = SubstatusShipFailed
	if _, err := env.svc.Receive(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Ship(ctx, o.ID, "", ""); err == nil {
		t.Fatal("substatus=ship_failed 应报错（并进异常池）")
	}
	if got := env.orderStatus(t, o.ID); got != order.StatusAtRelay {
		t.Fatalf("ship_failed 时状态应停在 at_relay，实际 %s", got)
	}
	codes := env.openExceptionCodes(t, o.ID)
	if len(codes) != 1 || codes[0] != order.CodeShipFailed {
		t.Fatalf("应有 1 条 ship_failed 异常，实际 %v", codes)
	}
}

// 验收：按 tpl_integration_type 决定是否传单号（经 B）。
func TestTrackingRulesByTpl(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()

	// 1) 3pl_tracking：要传单号。
	o1 := env.seedOrder(t, "tpl-3pl", order.Tpl3PLTracking, &env.relay.ID)
	if _, err := env.svc.Receive(ctx, o1.ID); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Ship(ctx, o1.ID, "CDEK123456", "CDEK")
	if err != nil {
		t.Fatalf("备货失败: %v", err)
	}
	if res.TrackingAction != "set" {
		t.Errorf("3pl_tracking 的动作应为 set，实际 %s", res.TrackingAction)
	}
	if len(env.fl.trackingSent) != 1 || env.fl.trackingSent[0] != "CDEK123456" {
		t.Fatalf("应把单号传给 Ozon，实际 %v", env.fl.trackingSent)
	}
	sh, _ := env.repo.ByOrder(ctx, o1.ID)
	if sh.TrackingSource == nil || *sh.TrackingSource != TrackingSourceSeller || sh.TrackingNo == nil {
		t.Fatalf("应记 seller 与我们传的单号: %+v", sh)
	}

	// 2) aggregator：只读不传 —— 单独传单号接口也要拒绝。
	o2 := env.seedOrder(t, "tpl-agg", order.TplAggregator, &env.relay.ID)
	if _, err := env.svc.SetTracking(ctx, o2.ID, "XX123456", ""); err == nil {
		t.Fatal("aggregator 的单号由 Ozon 登记，我们不该能传")
	}

	// 3) hybrid：进异常池。
	o3 := env.seedOrder(t, "tpl-hybrid", order.TplHybrid, &env.relay.ID)
	if _, err := env.svc.Receive(ctx, o3.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Ship(ctx, o3.ID, "", ""); err != nil {
		t.Fatalf("hybrid 备货本身应成功（单号进异常池）: %v", err)
	}
	codes := env.openExceptionCodes(t, o3.ID)
	if len(codes) != 1 || codes[0] != order.CodeHybridTpl {
		t.Fatalf("hybrid 应进异常池，实际 %v", codes)
	}
}

// 验收：面单取用（不落盘，按需拉取）。
func TestLabelFetch(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "label-1", order.TplOzon, &env.relay.ID)

	pdf, filename, err := env.svc.Label(ctx, o.ID)
	if err != nil {
		t.Fatalf("取面单失败: %v", err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF") || !strings.Contains(filename, "label-1") {
		t.Fatalf("面单内容/文件名不对: %q / %s", string(pdf), filename)
	}
	sh, err := env.repo.ByOrder(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sh.LabelRef == nil || !strings.Contains(*sh.LabelRef, "label-1") {
		t.Fatalf("应记面单引用: %+v", sh.LabelRef)
	}
}

// 验收：货代仓交接对照表含国内快递号 ↔ posting_number ↔ 面单引用。
func TestHandoverCSV(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "handover-1", order.TplOzon, &env.relay.ID)

	// 造一条人工回填的采购单（国内快递号）。
	task := &purchase.PurchaseTask{
		ID: snowflake.GenStringID(), OrderID: o.ID, Channel: catalog.ChannelManual,
		ExecutorType: purchase.ExecutorManual, Status: purchase.StatusShipped,
		Payload: []byte(`{"posting_number":"handover-1"}`),
	}
	if err := env.db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	carrier, tracking := "顺丰", "SF9876543210"
	if err := env.db.Create(&purchase.PurchaseOrder{
		ID: snowflake.GenStringID(), TaskID: task.ID, PlatformOrderID: "1234567890123",
		Amount: decimal.RequireFromString("9.90"), Currency: "CNY",
		DomesticCarrier: &carrier, DomesticTrackingNo: &tracking,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 订单推到 inbound（货在路上）才进交接表。
	if _, err := env.orders.Advance(ctx, o.ID, order.StatusInbound); err != nil {
		t.Fatal(err)
	}

	csvData, err := env.svc.HandoverCSV(ctx, env.relay.ID)
	if err != nil {
		t.Fatalf("导出对照表失败: %v", err)
	}
	text := string(csvData)
	if !strings.Contains(text, "handover-1") || !strings.Contains(text, "SF9876543210") {
		t.Fatalf("对照表应含 posting 与国内快递号:\n%s", text)
	}
	if !strings.Contains(text, "面单下载") {
		t.Errorf("对照表应有面单列:\n%s", text)
	}
	// 交接表不含买家信息（列头也不该有）。
	for _, banned := range []string{"买家", "收件人"} {
		if strings.Contains(text, banned) {
			t.Errorf("交接表不该出现 %q:\n%s", banned, text)
		}
	}
}

// 列表：打包页只看到货已出发的订单，且带上国内段单号与传单号动作。
func TestListPackingQueue(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "queue-1", order.Tpl3PLTracking, &env.relay.ID)
	if _, err := env.orders.Advance(ctx, o.ID, order.StatusInbound); err != nil {
		t.Fatal(err)
	}
	// 一张还没采购完的单不该出现在打包页。
	early := env.seedOrder(t, "queue-early", order.TplOzon, &env.relay.ID)
	if _, err := env.orders.Advance(ctx, early.ID, order.StatusPurchasing); err != nil {
		t.Fatal(err)
	}

	items, total, err := env.svc.List(ctx, ListFilter{RelayPointID: env.relay.ID})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].PostingNumber != "queue-1" {
		t.Fatalf("打包页应只含 queue-1，实际 total=%d items=%+v", total, items)
	}
	if items[0].TrackingAction != "set" {
		t.Errorf("3pl_tracking 的动作应为 set，实际 %s", items[0].TrackingAction)
	}
}

// ---- 重审回归（PR #16 重审发现，逐条钉住）----

// 重审 #8：并发取用发运记录只会有一行（唯一键 + 撞键重查）。
func TestGetOrCreateSingleRowUnderConcurrency(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "one-shipment", order.TplOzon, &env.relay.ID)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := env.repo.GetOrCreate(ctx, o.ID); err != nil {
				t.Errorf("GetOrCreate 失败: %v", err)
			}
		}()
	}
	wg.Wait()

	var n int64
	env.db.Model(&Shipment{}).Where("order_id = ?", o.ID).Count(&n)
	if n != 1 {
		t.Fatalf("一个订单只应有一条发运记录，实际 %d 行", n)
	}
}

// 重审 #6：交接对照表翻页取全，不静默截断在 200 行。
func TestHandoverCSVExportsAllPages(t *testing.T) {
	env := newShipEnv(t)
	ctx := context.Background()

	const total = 205 // 超过单页上限 200
	now := time.Now().UTC()
	rows := make([]order.Order, 0, total)
	relayID := env.relay.ID
	for i := 0; i < total; i++ {
		rows = append(rows, order.Order{
			ID: snowflake.GenStringID(), StoreID: env.shop.ID,
			PostingNumber: fmt.Sprintf("bulk-%03d", i), Status: order.StatusInbound,
			OzonStatus: order.OzonAwaitingPackaging, RelayPointID: &relayID,
			Currency: "CNY", CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := env.db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}

	csvData, err := env.svc.HandoverCSV(ctx, relayID)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(string(csvData), "bulk-")
	if got != total {
		t.Fatalf("对照表应含全部 %d 单，实际 %d 单（翻页没取全）", total, got)
	}
}
