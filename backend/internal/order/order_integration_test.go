//go:build integration

package order

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/hibiken/asynq"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ---- 假实现（S1-B / 队列未接入时的替身）----

type fakeSource struct {
	listed         []Posting
	unfulfilled    []Posting
	details        map[string]*Posting
	listErr        error
	unfulfilledErr error
}

func (f *fakeSource) ListPostings(context.Context, ListPostingsRequest) ([]Posting, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listed, nil
}

func (f *fakeSource) ListUnfulfilled(context.Context, ListPostingsRequest) ([]Posting, error) {
	if f.unfulfilledErr != nil {
		return nil, f.unfulfilledErr
	}
	return f.unfulfilled, nil
}

func (f *fakeSource) GetPosting(_ context.Context, postingNumber string) (*Posting, error) {
	if p, ok := f.details[postingNumber]; ok {
		return p, nil
	}
	return nil, errors.New("详情不存在")
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

func (f *fakeEnqueuer) types() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, t.Type+":"+t.IdempotencyKey)
	}
	return out
}

func (f *fakeEnqueuer) hasType(taskType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.Type == taskType {
			return true
		}
	}
	return false
}

func (f *fakeEnqueuer) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = nil
}

// ---- 测试环境 ----

type orderEnv struct {
	db    *gorm.DB
	svc   *Service
	repo  *Repo
	exc   *Exceptions
	shops *store.Repo
	creds *store.CredentialService
	shop  *store.Shop
	src   *fakeSource
	q     *fakeEnqueuer
	now   time.Time
}

func newOrderEnv(t *testing.T) *orderEnv {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake: %v", err)
	}
	db := testutil.MySQL(t)
	testutil.Truncate(t, db)
	v := testutil.Vault(t)
	rec := audit.New(db)

	shops := store.NewRepo(db)
	creds := store.NewCredentialService(db, v, rec)

	// 店铺 + Ozon 凭据（拉单要凭据）。
	shop := &store.Shop{
		ID: snowflake.GenStringID(), Name: "测试店", Mode: store.ModeRFBS,
		ClientID: "client-1", Currency: "CNY", Status: store.ShopStatusActive,
	}
	if err := shops.Create(context.Background(), shop); err != nil {
		t.Fatalf("建店: %v", err)
	}
	if _, err := creds.Put(context.Background(), shop.ID, store.KindOzonAPIKey,
		map[string]string{"api_key": "test-key-123456"}, nil); err != nil {
		t.Fatalf("建凭据: %v", err)
	}

	src := &fakeSource{details: map[string]*Posting{}}
	que := &fakeEnqueuer{}
	repo := NewRepo(db)
	exc := NewExceptions(db)
	svc := NewService(repo, exc, shops, creds, func(store.Shop, *store.Decrypted) (PostingSource, error) {
		return src, nil
	}, que)

	return &orderEnv{
		db: db, svc: svc, repo: repo, exc: exc, shops: shops, creds: creds,
		shop: shop, src: src, q: que, now: time.Now().UTC(),
	}
}

func posting(pn, status string, items ...PostingItem) Posting {
	return Posting{
		PostingNumber:      pn,
		OrderNumber:        "ORD-" + pn,
		Status:             status,
		TplIntegrationType: TplOzon,
		Currency:           "CNY",
		Items:              items,
	}
}

func item(offerID string, qty int, price string) PostingItem {
	return PostingItem{OzonOfferID: offerID, Qty: qty, Price: decimal.RequireFromString(price), Currency: "CNY"}
}

// 验收：同一 posting 重复拉只入库一次；拉单成功后写回 stores.last_sync_at；
// 允许采购的状态会投采购计划任务。
func TestPollIdempotentAndWritesLastSyncAt(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("0123-1", OzonAwaitingPackaging, item("SKU-1", 2, "10.00"))}
	env.src.details["0123-1"] = func() *Posting {
		p := posting("0123-1", OzonAwaitingPackaging, item("SKU-1", 2, "10.00"))
		return &p
	}()

	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatalf("第一次拉单失败: %v", err)
	}
	// 第二次：同一 posting 再来一遍（模拟下一轮轮询）。
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatalf("第二次拉单失败: %v", err)
	}

	orders, total, err := env.repo.List(ctx, OrderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(orders) != 1 {
		t.Fatalf("同一 posting 重复拉应只有 1 单，实际 %d", total)
	}
	items, err := env.repo.Items(ctx, orders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Qty != 2 {
		t.Fatalf("商品行不对: %+v", items)
	}
	if !orders[0].TotalAmount.Equal(decimal.RequireFromString("20.00")) {
		t.Errorf("订单总额 = %s，期望 20.00", orders[0].TotalAmount)
	}

	fresh, err := env.shops.ByID(ctx, env.shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.LastSyncAt == nil {
		t.Fatal("拉单成功后应写回 stores.last_sync_at")
	}

	// awaiting_packaging → 投过采购计划任务（幂等键 = 订单）。
	found := false
	for _, s := range env.q.types() {
		if s == TaskTypePurchasePlan+":purchase:plan:"+orders[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("应投采购计划任务，实际入队: %v", env.q.types())
	}
}

// 验收：「平台未放行」不生成采购任务；订单建立为 new。
func TestPollHeldStatusCreatesOrderButNoPurchaseTask(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("held-1", OzonAwaitingRegistration, item("SKU-1", 1, "9.99"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatalf("拉单失败: %v", err)
	}
	orders, total, err := env.repo.List(ctx, OrderFilter{})
	if err != nil || total != 1 {
		t.Fatalf("应建 1 单，实际 %d（err=%v）", total, err)
	}
	if orders[0].Status != StatusNew {
		t.Errorf("未放行订单内部状态应为 new，实际 %s", orders[0].Status)
	}
	if len(env.q.tasks) != 0 {
		t.Fatalf("未放行不应投采购任务，实际: %v", env.q.types())
	}

	// 平台放行后（awaiting_packaging）再来一轮：这次要投采购任务。
	env.src.listed = []Posting{posting("held-1", OzonAwaitingPackaging, item("SKU-1", 1, "9.99"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatalf("第二轮拉单失败: %v", err)
	}
	if len(env.q.tasks) == 0 {
		t.Fatal("放行后应投采购计划任务")
	}
}

// 验收：表外状态进异常池（同一对象同一 code 去重）；仲裁状态同样进池。
func TestPollUnknownStatusRaisesExceptionAndDedupes(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("weird-1", "some_new_status", item("SKU-1", 1, "1.00"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	rows, total, err := env.exc.List(ctx, ExceptionFilter{Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("同一对象同一 code 应去重为 1 条，实际 %d", total)
	}
	if rows[0].Code != CodeUnknownStatus {
		t.Errorf("异常码 = %s，期望 %s", rows[0].Code, CodeUnknownStatus)
	}

	// 仲裁：另一单，进 arbitration 异常。
	env.src.listed = []Posting{posting("arb-1", OzonArbitration, item("SKU-1", 1, "1.00"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	_, total2, err := env.exc.List(ctx, ExceptionFilter{Code: CodeArbitration, Status: "all"})
	if err != nil || total2 != 1 {
		t.Fatalf("仲裁应收 1 条异常，实际 %d（err=%v）", total2, err)
	}
}

// 状态映射落到库：awaiting_deliver → handed_over（只前进，不回退）。
func TestPollForwardStatusAdvances(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("adv-1", OzonAwaitingDeliver, item("SKU-1", 1, "1.00"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	o, err := env.repo.ByPosting(ctx, env.shop.ID, "adv-1")
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusHandedOver {
		t.Fatalf("awaiting_deliver 应推进到 handed_over，实际 %s", o.Status)
	}

	// 再来一轮同样的状态：保持 handed_over，不重复动作。
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	o2, _ := env.repo.ByPosting(ctx, env.shop.ID, "adv-1")
	if o2.Status != StatusHandedOver {
		t.Fatalf("重复拉单不该改变状态，实际 %s", o2.Status)
	}
}

// 验收：S1 各类超时异常能写入 exceptions（超时未采购 / 发货截止临近 / 中转点停滞）。
func TestSweepRaisesTimeoutExceptions(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1) 超时未采购：允许采购但 25 小时没动。
	stuck := &Order{
		ID: snowflake.GenStringID(), StoreID: env.shop.ID, PostingNumber: "stuck-1",
		Status: StatusNew, OzonStatus: OzonAwaitingPackaging, Currency: "CNY",
		CreatedAt: now.Add(-30 * time.Hour), UpdatedAt: now.Add(-25 * time.Hour),
	}
	if err := env.db.Create(stuck).Error; err != nil {
		t.Fatal(err)
	}
	// 2) 发货截止临近：20 小时后截止，还没交运。
	dl := now.Add(20 * time.Hour)
	deadlineSoon := &Order{
		ID: snowflake.GenStringID(), StoreID: env.shop.ID, PostingNumber: "late-1",
		Status: StatusInbound, OzonStatus: OzonAwaitingPackaging, Currency: "CNY",
		ShipDeadline: &dl, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
	}
	if err := env.db.Create(deadlineSoon).Error; err != nil {
		t.Fatal(err)
	}
	// 3) 中转点停滞：签收 25 小时未交运。
	relay := &Order{
		ID: snowflake.GenStringID(), StoreID: env.shop.ID, PostingNumber: "relay-1",
		Status: StatusAtRelay, OzonStatus: OzonAwaitingDeliver, Currency: "CNY",
		CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-25 * time.Hour),
	}
	if err := env.db.Create(relay).Error; err != nil {
		t.Fatal(err)
	}

	if err := env.svc.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	assertException := func(refID, code string) {
		t.Helper()
		var n int64
		env.db.Model(&Exception{}).Where("ref_id = ? AND code = ?", refID, code).Count(&n)
		if n != 1 {
			t.Errorf("对象 %s 应有 1 条 %s 异常，实际 %d", refID, code, n)
		}
	}
	assertException(stuck.ID, CodePurchaseTimeout)
	assertException(deadlineSoon.ID, CodeShipDeadlineNear)
	assertException(relay.ID, CodeRelayStalled)
}

// 补投规则：停在「允许采购但没动」的订单会被重新投递。
func TestRescanRuleFindsStuckOrders(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	stuck := &Order{
		ID: snowflake.GenStringID(), StoreID: env.shop.ID, PostingNumber: "rescan-1",
		Status: StatusNew, OzonStatus: OzonAwaitingPackaging, Currency: "CNY",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-10 * time.Minute),
	}
	if err := env.db.Create(stuck).Error; err != nil {
		t.Fatal(err)
	}
	tasks, err := env.svc.RescanRule().Rescan(ctx, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Type != TaskTypePurchasePlan {
		t.Fatalf("应补投 1 条采购计划任务，实际 %+v", tasks)
	}
	if tasks[0].IdempotencyKey != "purchase:plan:"+stuck.ID {
		t.Errorf("幂等键不对: %s", tasks[0].IdempotencyKey)
	}
}

// 异常池：手动处理 + 处理人留痕。
func TestExceptionResolve(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	if err := env.exc.Raise(ctx, RefOrder, "order-x", CodeShipFailed, "备货没成"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := env.exc.List(ctx, ExceptionFilter{RefID: "order-x"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("应有 1 条未处理异常，实际 %d（err=%v）", len(rows), err)
	}
	row, err := env.exc.Resolve(ctx, rows[0].ID, "已人工重试备货", "boss")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != ExceptionResolved || row.HandledBy == nil || *row.HandledBy != "boss" || row.HandledAt == nil {
		t.Fatalf("处理留痕不完整: %+v", row)
	}
	// 已处理的再点一次不报错。
	if _, err := env.exc.Resolve(ctx, row.ID, "", "boss"); err != nil {
		t.Fatalf("重复处理不该报错: %v", err)
	}
}

// ---- 重审回归（PR #16 重审发现，逐条钉住）----

// fakeNotifier 记下异常告警（总纲 §5.2「自动进池 + 通知」）。
type fakeNotifier struct {
	mu   sync.Mutex
	keys []string
}

func (f *fakeNotifier) Notify(dedupeKey, _, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, dedupeKey)
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

// 重审 #1/#3：内容没变时拉单不许刷新 updated_at，否则超时扫描永不触发。
func TestPollDoesNotRefreshUpdatedAtSoSweepFires(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("stale-1", OzonAwaitingPackaging, item("SKU-1", 1, "1.00"))}
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	o, err := env.repo.ByPosting(ctx, env.shop.ID, "stale-1")
	if err != nil {
		t.Fatal(err)
	}

	// 把这单拨老 25 小时（模拟「一直没动静」），此后再拉两轮（Ozon 侧数据没变）。
	stale := time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Millisecond)
	if err := env.db.Model(&Order{}).Where("id = ?", o.ID).Update("updated_at", stale).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
			t.Fatal(err)
		}
	}

	after, err := env.repo.ByID(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(stale) {
		t.Fatalf("内容没变时不该刷新 updated_at（计时器会失真）：%v → %v", stale, after.UpdatedAt)
	}

	// 扫描必须能扫到它（旧实现在这里一条都不报）。
	if err := env.svc.HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var n int64
	env.db.Model(&Exception{}).Where("ref_id = ? AND code = ?", o.ID, CodePurchaseTimeout).Count(&n)
	if n != 1 {
		t.Fatalf("应报 1 条超时未采购，实际 %d", n)
	}
}

// 重审 #2：并发 upsert 商品行只会有一行，数量绝不翻倍。
func TestUpsertItemsIdempotentUnderConcurrentWrites(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	o, _, err := env.repo.UpsertFromOzon(ctx, env.shop.ID, posting("items-1", OzonAwaitingPackaging))
	if err != nil {
		t.Fatal(err)
	}
	items := []PostingItem{item("SKU-1", 2, "10.00")}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = env.repo.UpsertItems(ctx, o.ID, items)
		}()
	}
	wg.Wait()
	// 再顺序来一次。
	if err := env.repo.UpsertItems(ctx, o.ID, items); err != nil {
		t.Fatal(err)
	}

	rows, err := env.repo.Items(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("同一 (order, offer) 只应有一行，实际 %d 行（重复行会让采购任务买双份）", len(rows))
	}
	if rows[0].Qty != 2 {
		t.Fatalf("数量被叠加了：%d，期望 2", rows[0].Qty)
	}
}

// 重审 #9：拉未完成单失败要让整轮失败，不能悄悄前移同步游标。
func TestUnfulfilledFailureFailsPollAndKeepsCursor(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	env.src.listed = []Posting{posting("uf-1", OzonAwaitingPackaging, item("SKU-1", 1, "1.00"))}
	env.src.unfulfilledErr = errors.New("429 限流")
	if err := env.svc.PollStore(ctx, env.shop.ID); err == nil {
		t.Fatal("未完成单拉失败应让整轮失败（否则老单状态变化永久漏接）")
	}
	fresh, err := env.shops.ByID(ctx, env.shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.LastSyncAt != nil {
		t.Fatal("本轮失败不该写 last_sync_at：游标前移会把还没扫到的老单甩出窗口")
	}

	// 下一轮恢复正常：窗口仍覆盖这批单，能正常入库。
	env.src.unfulfilledErr = nil
	if err := env.svc.PollStore(ctx, env.shop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.repo.ByPosting(ctx, env.shop.ID, "uf-1"); err != nil {
		t.Fatalf("恢复后应能正常入库: %v", err)
	}
}

// 重审 #11：唯一键被软删历史行占用时，报错而不是无限递归（栈溢出会带崩整个进程）。
func TestSoftDeletedOrderDoesNotRecurse(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	dead := &Order{
		ID: snowflake.GenStringID(), StoreID: env.shop.ID, PostingNumber: "dead-1",
		Status: StatusCancelled, OzonStatus: OzonCancelled, Currency: "CNY", DelFlag: true,
	}
	if err := env.db.Create(dead).Error; err != nil {
		t.Fatal(err)
	}

	_, _, err := env.repo.UpsertFromOzon(ctx, env.shop.ID, posting("dead-1", OzonAwaitingPackaging))
	if err == nil {
		t.Fatal("唯一键被软删行占用时应明确报错")
	}
	if !strings.Contains(err.Error(), "软删") {
		t.Fatalf("错误信息要指认原因（软删历史行占键），实际: %v", err)
	}
}

// 重审 #3（路一）：异常进池要同时告警（总纲 §5.2「自动进池 + 通知」）。
func TestExceptionRaiseNotifies(t *testing.T) {
	env := newOrderEnv(t)
	ctx := context.Background()

	n := &fakeNotifier{}
	env.exc.SetNotifier(n)
	if err := env.exc.Raise(ctx, RefOrder, "order-n1", CodeArbitration, "仲裁"); err != nil {
		t.Fatal(err)
	}
	// 同一对象同一码重复报：库里去了重，告警也只有一次。
	if err := env.exc.Raise(ctx, RefOrder, "order-n1", CodeArbitration, "仲裁（重复）"); err != nil {
		t.Fatal(err)
	}
	if n.count() != 1 {
		t.Fatalf("去重后只应告警 1 次，实际 %d", n.count())
	}
}
