//go:build integration

package purchase

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
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/hibiken/asynq"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ---- 假实现 ----

type fakeBuyer struct {
	preview     *PreviewResult
	previewErr  error
	createErr   error
	createCalls int
	placed      []BuyerOrder
}

func (f *fakeBuyer) PreviewOrder(context.Context, PreviewRequest) (*PreviewResult, error) {
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	if f.preview == nil {
		return &PreviewResult{CanOrder: true}, nil
	}
	return f.preview, nil
}

func (f *fakeBuyer) CreateOrder(_ context.Context, req CreateOrderRequest) (*BuyerOrder, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.createCalls++
	po := BuyerOrder{
		PlatformOrderID: fmt.Sprintf("PO-%d", f.createCalls),
		Amount:          decimal.RequireFromString("12.34"),
		Currency:        "CNY",
		Remark:          req.Remark,
		CreatedAt:       time.Now().UTC(),
	}
	if !strings.Contains(req.Remark, "-") && req.Remark == "" {
		return nil, fmt.Errorf("下单必须带 posting_number 备注")
	}
	f.placed = append(f.placed, po)
	return &po, nil
}

func (f *fakeBuyer) GetOrder(_ context.Context, platformOrderID string) (*BuyerOrder, error) {
	for i := range f.placed {
		if f.placed[i].PlatformOrderID == platformOrderID {
			return &f.placed[i], nil
		}
	}
	return nil, fmt.Errorf("订单不存在: %s", platformOrderID)
}

func (f *fakeBuyer) ListBuyerOrders(context.Context, ListBuyerOrdersRequest) ([]BuyerOrder, error) {
	return f.placed, nil
}

func (f *fakeBuyer) GetLogistics(_ context.Context, platformOrderID string) (*Logistics, error) {
	return &Logistics{Carrier: "顺丰", TrackingNo: "SF" + platformOrderID}, nil
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

// ---- 测试环境 ----

type purchaseEnv struct {
	db        *gorm.DB
	svc       *Service
	repo      *Repo
	orders    *order.Service
	orderRepo *order.Repo
	relays    *order.RelayRepo
	cat       *catalog.Repo
	shop      *store.Shop
	relay     *order.RelayPoint
	buyer     *fakeBuyer
	q         *fakeEnqueuer
}

func newPurchaseEnv(t *testing.T) *purchaseEnv {
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
		ID: snowflake.GenStringID(), Name: "采购测试店", Mode: store.ModeRFBS,
		ClientID: "c1", Currency: "CNY", Status: store.ShopStatusActive,
	}
	if err := shops.Create(ctx, shop); err != nil {
		t.Fatalf("建店: %v", err)
	}

	relays := order.NewRelayRepo(db)
	relay := &order.RelayPoint{
		ID: snowflake.GenStringID(), Name: "深圳货代仓", Kind: order.RelayKindForwarder,
		Address: "广东省深圳市宝安区某路 1 号", Contact: "张三 13800000000", Status: order.RelayStatusActive,
	}
	if err := relays.Create(ctx, relay); err != nil {
		t.Fatalf("建中转点: %v", err)
	}
	shop.DefaultRelayPointID = &relay.ID
	if err := shops.Update(ctx, shop); err != nil {
		t.Fatalf("设置店铺默认中转点: %v", err)
	}

	// 1688 企业级凭据（自动执行器要用）。
	if _, err := creds.Put(ctx, "", store.KindAlibabaToken, map[string]string{"access_token": "tok-abcdefg"}, nil); err != nil {
		t.Fatalf("建 1688 token: %v", err)
	}

	buyer := &fakeBuyer{}
	que := &fakeEnqueuer{}
	orderRepo := order.NewRepo(db)
	exc := order.NewExceptions(db)
	orderSvc := order.NewService(orderRepo, exc, shops, creds, nil, que)
	repo := NewRepo(db)
	svc := NewService(repo, orderSvc, relays, catalog.NewRepo(db), creds,
		func(BuyerCredentials) (BuyerClient, error) { return buyer, nil }, rec, que)

	return &purchaseEnv{
		db: db, svc: svc, repo: repo, orders: orderSvc, orderRepo: orderRepo,
		relays: relays, cat: catalog.NewRepo(db), shop: shop, relay: relay, buyer: buyer, q: que,
	}
}

// seedOrder 造一张可采购的订单（awaiting_packaging，状态 new）；
// withMapping = 顺带给 sku 建一条 1688 自用版映射。
func (e *purchaseEnv) seedOrder(t *testing.T, postingNumber, sku string, withMapping bool) *order.Order {
	t.Helper()
	ctx := context.Background()
	p := order.Posting{
		PostingNumber:      postingNumber,
		OrderNumber:        "ORD-" + postingNumber,
		Status:             order.OzonAwaitingPackaging,
		TplIntegrationType: order.TplOzon,
		Currency:           "CNY",
		Items: []order.PostingItem{
			{OzonOfferID: sku, Qty: 2, Price: decimal.RequireFromString("10.00"), Currency: "CNY"},
		},
	}
	o, _, err := e.orderRepo.UpsertFromOzon(ctx, e.shop.ID, p)
	if err != nil {
		t.Fatalf("造订单: %v", err)
	}
	// 商品行与轮询同路径入库（applyPosting 里 UpsertFromOzon 之后就是这一步）。
	if err := e.orderRepo.UpsertItems(ctx, o.ID, p.Items); err != nil {
		t.Fatalf("造订单行: %v", err)
	}
	items, err := e.orderRepo.Items(ctx, o.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("订单行没建出来: %v", err)
	}
	if withMapping {
		e.mapOffer(t, sku, catalog.Platform1688, catalog.ChannelSelfUse)
	}
	return o
}

// mapOffer 给 sku 建「货源商品 + 按店映射」。
func (e *purchaseEnv) mapOffer(t *testing.T, sku, platform, channel string) *catalog.SupplierOffer {
	t.Helper()
	ctx := context.Background()
	offer := &catalog.SupplierOffer{
		ID: snowflake.GenStringID(), Platform: platform, ItemID: "item-" + sku,
		SkuID: "spec-1", URL: "https://detail.1688.com/offer/6789.html",
		PurchasePrice: decimal.RequireFromString("8.50"), Currency: "CNY",
		OrderChannel: channel, Status: catalog.StatusActive,
	}
	if err := e.cat.CreateOffer(ctx, offer); err != nil {
		t.Fatalf("建货源: %v", err)
	}
	if err := e.cat.CreateLink(ctx, &catalog.OfferLink{
		ID: snowflake.GenStringID(), StoreID: e.shop.ID, OzonOfferID: sku,
		SupplierOfferID: offer.ID, Priority: 1, TargetStock: 10,
	}); err != nil {
		t.Fatalf("建映射: %v", err)
	}
	return offer
}

func (e *purchaseEnv) orderStatus(t *testing.T, orderID string) string {
	t.Helper()
	o, err := e.orderRepo.ByID(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	return o.Status
}

func (e *purchaseEnv) openExceptions(t *testing.T, orderID string) []order.Exception {
	t.Helper()
	rows, _, err := e.orders.Exceptions().List(context.Background(), order.ExceptionFilter{RefID: orderID, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// 验收：新订单生成采购任务、订单推进 purchasing、订单行回填映射。
func TestPlanCreatesTaskAndAdvancesOrder(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "plan-1", "SKU-A", true)

	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatalf("生成采购任务失败: %v", err)
	}

	tasks, err := env.repo.TasksByOrder(ctx, o.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("应有 1 个采购任务，实际 %d（err=%v）", len(tasks), err)
	}
	task := tasks[0]
	if task.ExecutorType != ExecutorAuto || task.Channel != catalog.ChannelSelfUse || task.Status != StatusPending {
		t.Errorf("任务初始状态不对: %+v", task)
	}
	pay, err := task.DecodePayload()
	if err != nil {
		t.Fatal(err)
	}
	if pay.PostingNumber != "plan-1" || pay.RelayAddress == "" || len(pay.Items) != 1 {
		t.Errorf("载荷快照不对: %+v", pay)
	}
	if env.orderStatus(t, o.ID) != order.StatusPurchasing {
		t.Errorf("订单应推进到 purchasing，实际 %s", env.orderStatus(t, o.ID))
	}
	if !env.q.has(TaskTypeExecute) {
		t.Error("自动任务应投执行任务")
	}
	// 订单行回填认到的映射。
	items, _ := env.orderRepo.Items(ctx, o.ID)
	if items[0].OfferLinkID == nil {
		t.Error("order_items.offer_link_id 应回填")
	}

	// 幂等：再跑一次 plan 不重复建任务。
	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks2, _ := env.repo.TasksByOrder(ctx, o.ID)
	if len(tasks2) != 1 {
		t.Fatalf("重复 plan 不该多建任务，实际 %d", len(tasks2))
	}
}

// 没有货源映射 → 订单级异常，不建任务。
func TestPlanWithoutMappingRaisesException(t *testing.T) {
	env := newPurchaseEnv(t)
	o := env.seedOrder(t, "plan-2", "SKU-A", false)

	if err := env.svc.PlanOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	rows := env.openExceptions(t, o.ID)
	if len(rows) != 1 || rows[0].Code != order.CodeOfferMappingMissing {
		t.Fatalf("应记 1 条 offer_mapping_missing，实际 %+v", rows)
	}
	if env.orderStatus(t, o.ID) != order.StatusNew {
		t.Error("没有映射时订单不该推进")
	}
}

// 没有中转点 → 地址校验失败异常。
func TestPlanWithoutRelayRaisesAddressInvalid(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "plan-3", "SKU-A", true)

	// 去掉店铺默认中转点。
	shop, _ := env.orders.Shop(ctx, env.shop.ID)
	shop.DefaultRelayPointID = nil
	if err := env.db.Model(&store.Shop{}).Where("id = ?", shop.ID).Update("default_relay_point_id", nil).Error; err != nil {
		t.Fatal(err)
	}

	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	rows := env.openExceptions(t, o.ID)
	if len(rows) != 1 || rows[0].Code != order.CodeAddressInvalid {
		t.Fatalf("应记 1 条 address_invalid，实际 %+v", rows)
	}
}

// 验收：自动任务下单成功停在 ordered；重复执行不重复下单；
// 模拟「下单成功后崩溃」（任务停在 executing、1688 已有单）→ 重启后补记、不重复下单。
func TestExecuteAutoAndCrashRecovery(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "exec-1", "SKU-A", true)

	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := env.repo.TasksByOrder(ctx, o.ID)
	taskID := tasks[0].ID

	// 1) 正常执行：下单 1 次、任务停 ordered、采购单入库、订单推进 purchased。
	if err := env.svc.Execute(ctx, taskID); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if env.buyer.createCalls != 1 {
		t.Fatalf("应下 1 次单，实际 %d", env.buyer.createCalls)
	}
	task, _ := env.repo.TaskByID(ctx, taskID)
	if task.Status != StatusOrdered {
		t.Fatalf("下单成功后任务应停在 ordered（等人工记已付款），实际 %s", task.Status)
	}
	po, _ := env.repo.PurchaseOrderByTask(ctx, taskID)
	if po == nil || po.PlatformOrderID == "" {
		t.Fatal("采购单应入库")
	}
	if env.orderStatus(t, o.ID) != order.StatusPurchased {
		t.Errorf("订单应推进到 purchased，实际 %s", env.orderStatus(t, o.ID))
	}

	// 2) 重复执行（人工点了两次执行 / 补投扫描重复投）：不该再下单。
	if err := env.svc.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if env.buyer.createCalls != 1 {
		t.Fatalf("重复执行不应再下单，实际下单 %d 次", env.buyer.createCalls)
	}

	// 3) 模拟「下单成功后崩溃」：新任务落到 executing，1688 侧已有该 posting 的单。
	o2 := env.seedOrder(t, "exec-crash-1", "SKU-A", true)
	if err := env.svc.PlanOrder(ctx, o2.ID); err != nil {
		t.Fatal(err)
	}
	tasks2, _ := env.repo.TasksByOrder(ctx, o2.ID)
	task2 := tasks2[0]
	// 崩溃现场：进程在下单成功后挂掉 —— 任务停在 executing，采购单还没写。
	if err := env.repo.SetStatus(ctx, task2.ID, StatusExecuting); err != nil {
		t.Fatal(err)
	}
	// 1688 那边已经有单了（备注里带 posting_number）。
	env.buyer.placed = append(env.buyer.placed, BuyerOrder{
		PlatformOrderID: "PO-CRASH", Amount: decimal.RequireFromString("12.34"),
		Currency: "CNY", Remark: "posting exec-crash-1", CreatedAt: time.Now().UTC(),
	})
	callsBefore := env.buyer.createCalls

	if err := env.svc.Execute(ctx, task2.ID); err != nil {
		t.Fatalf("重启后执行失败: %v", err)
	}
	if env.buyer.createCalls != callsBefore {
		t.Fatalf("崩溃重启后不应重复下单（下单次数 %d → %d）", callsBefore, env.buyer.createCalls)
	}
	task2After, _ := env.repo.TaskByID(ctx, task2.ID)
	if task2After.Status != StatusOrdered {
		t.Fatalf("应补记到 ordered，实际 %s", task2After.Status)
	}
	po2, _ := env.repo.PurchaseOrderByTask(ctx, task2.ID)
	if po2 == nil || po2.PlatformOrderID != "PO-CRASH" {
		t.Fatalf("补记的采购单应指向 1688 已有订单，实际 %+v", po2)
	}
}

// 验收：新商家首单自动转人工。
func TestNewSellerConvertsToManual(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "newseller-1", "SKU-A", true)

	env.buyer.preview = &PreviewResult{CanOrder: false, Reason: PreviewReasonNewSeller, Message: "只能对老商家下单"}
	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := env.repo.TasksByOrder(ctx, o.ID)
	if err := env.svc.Execute(ctx, tasks[0].ID); err != nil {
		t.Fatalf("执行不该报错（转人工是正常分支）: %v", err)
	}
	task, _ := env.repo.TaskByID(ctx, tasks[0].ID)
	if task.ExecutorType != ExecutorManual || task.Status != StatusPending {
		t.Fatalf("应转人工且回到 pending，实际 %s/%s", task.ExecutorType, task.Status)
	}
	pay, _ := task.DecodePayload()
	if pay.Note == "" {
		t.Error("转人工应在载荷里记原因")
	}
	if env.buyer.createCalls != 0 {
		t.Error("转人工后不应下单")
	}

	// 备料单里应带上 posting_number（人工照着下单用）。
	sheet, err := env.svc.MaterialSheetFor(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sheet.Text, "newseller-1") {
		t.Errorf("备料单应含 posting_number:\n%s", sheet.Text)
	}
	if sheet.Address != env.relay.Address {
		t.Errorf("备料单收货地址应 = 中转点地址，实际 %s", sheet.Address)
	}
}

// 验收：人工执行器回填（平台单号 / 实付 / 国内快递号）→ shipped + 订单 inbound；
// 记已付款 → paid。
func TestFillBackAndMarkPaid(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()

	// 人工任务：货源通道 = manual（拼多多 / 淘宝）。
	o := env.seedOrder(t, "manual-1", "SKU-A", false)
	env.mapOffer(t, "SKU-A", catalog.PlatformPDD, catalog.ChannelManual)
	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := env.repo.TasksByOrder(ctx, o.ID)
	task := tasks[0]
	if task.ExecutorType != ExecutorManual {
		t.Fatalf("manual 通道应出人工任务，实际 %s", task.ExecutorType)
	}

	// 格式不对 → 拒绝。
	if _, err := env.svc.FillBack(ctx, task.ID, FillBackRequest{
		PlatformOrderID: "123456789", Amount: decimal.NewFromInt(5), DomesticTrackingNo: "SF1",
	}); err == nil {
		t.Fatal("快递号格式不对应拒绝")
	}

	// 正常回填。
	if _, err := env.svc.FillBack(ctx, task.ID, FillBackRequest{
		PlatformOrderID: "1234567890123", Amount: decimal.RequireFromString("6.50"),
		DomesticCarrier: "顺丰", DomesticTrackingNo: "SF1234567890",
	}); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	got, _ := env.repo.TaskByID(ctx, task.ID)
	if got.Status != StatusShipped {
		t.Fatalf("回填后任务应 shipped，实际 %s", got.Status)
	}
	if env.orderStatus(t, o.ID) != order.StatusInbound {
		t.Errorf("订单应推进 inbound（国内段在途），实际 %s", env.orderStatus(t, o.ID))
	}
	po, _ := env.repo.PurchaseOrderByTask(ctx, task.ID)
	if po == nil || po.DomesticTrackingNo == nil || *po.DomesticTrackingNo != "SF1234567890" {
		t.Fatalf("国内段单号应入库: %+v", po)
	}

	// 自动任务：记已付款 → paid。
	o2 := env.seedOrder(t, "pay-1", "SKU-B", true)
	if err := env.svc.PlanOrder(ctx, o2.ID); err != nil {
		t.Fatal(err)
	}
	tasks2, _ := env.repo.TasksByOrder(ctx, o2.ID)
	if err := env.svc.Execute(ctx, tasks2[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.MarkPaid(ctx, tasks2[0].ID, decimal.RequireFromString("12.34"), nil); err != nil {
		t.Fatalf("记已付款失败: %v", err)
	}
	paid, _ := env.repo.TaskByID(ctx, tasks2[0].ID)
	if paid.Status != StatusPaid {
		t.Fatalf("记已付款后任务应 paid，实际 %s", paid.Status)
	}
	po2, _ := env.repo.PurchaseOrderByTask(ctx, tasks2[0].ID)
	if po2.PaidAt == nil {
		t.Error("采购单应记 paid_at")
	}
}

// 验收：中转点签收（订单 at_relay）后采购任务关单。
func TestCloseAfterRelay(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	o := env.seedOrder(t, "close-1", "SKU-A", true)

	if err := env.svc.PlanOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := env.repo.TasksByOrder(ctx, o.ID)
	if err := env.svc.Execute(ctx, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.FillBack(ctx, tasks[0].ID, FillBackRequest{
		PlatformOrderID: "1234567890123", Amount: decimal.RequireFromString("12.34"),
		DomesticTrackingNo: "SF1234567890",
	}); err != nil {
		t.Fatal(err)
	}

	// 签收前关单：不动。
	if err := env.svc.CloseOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := env.repo.TaskByID(ctx, tasks[0].ID)
	if got.Status == StatusClosed {
		t.Fatal("订单还没签收，不该关单")
	}

	// 模拟中转点签收（shipment 侧会把订单推到 at_relay）。
	if _, err := env.orders.Advance(ctx, o.ID, order.StatusAtRelay); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.CloseOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = env.repo.TaskByID(ctx, tasks[0].ID)
	if got.Status != StatusClosed {
		t.Fatalf("签收后应关单，实际 %s", got.Status)
	}
}

// 补投规则：卡住的自动任务会被重新投递；人工任务不算卡住。
func TestRescanRulesFindStuckTasks(t *testing.T) {
	env := newPurchaseEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	auto := &PurchaseTask{
		ID: snowflake.GenStringID(), OrderID: "o-1", Channel: catalog.ChannelSelfUse,
		ExecutorType: ExecutorAuto, Status: StatusExecuting, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-30 * time.Minute),
	}
	manual := &PurchaseTask{
		ID: snowflake.GenStringID(), OrderID: "o-2", Channel: catalog.ChannelManual,
		ExecutorType: ExecutorManual, Status: StatusPending, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-30 * time.Minute),
	}
	if err := env.db.Create(auto).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Create(manual).Error; err != nil {
		t.Fatal(err)
	}

	rules := env.svc.RescanRules()
	var executeTasks []queue.Task
	for _, r := range rules {
		if r.Name == "purchase:execute" {
			var err error
			executeTasks, err = r.Rescan(ctx, now.Add(-5*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(executeTasks) != 1 || executeTasks[0].IdempotencyKey != "purchase:execute:"+auto.ID {
		t.Fatalf("应只补投卡住的自动任务，实际 %+v", executeTasks)
	}
}
