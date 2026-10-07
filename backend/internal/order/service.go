// 订单域服务：轮询拉单（总纲 §5.2 每店每 5 分钟）、超时异常扫描、补投规则、
// 状态推进（purchase / shipment 也走这里，保证「只前进不后退」只有一处实现）。
package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/hibiken/asynq"
)

// 任务类型。
const (
	TaskTypePollStores    = "order:poll_stores" // 定时：找出该拉的店，按店投 TaskTypePoll
	TaskTypePoll          = "order:poll"        // 单店拉单
	TaskTypeSweep         = "order:sweep"       // 定时：超时异常扫描
	TaskTypePurchasePlan  = "purchase:plan"     // 投给 purchase：新订单生成采购任务
	TaskTypePurchaseClose = "purchase:close"    // 投给 purchase：中转点已签收，关采购任务
)

// 轮询间隔（总纲 §13.1）：未开推送的店 5 分钟；开推送的店对账 30 分钟。
// S1 全是未开推送的店（推送 S2 才接），Push 档先留着。
const (
	PollIntervalNoPush = 5 * time.Minute
	PollIntervalPush   = 30 * time.Minute
	pollOverlap        = 5 * time.Minute // 时间窗重叠，防边界丢单
	firstPollLookback  = 24 * time.Hour  // 首次拉单回看窗口
)

// 异常阈值（子 spec §6 自定细节；「超时未采购」本份补定，见 PR 说明）。
const (
	TimeoutPurchase = 24 * time.Hour // 允许采购后 24 小时还没推进
	DeadlineHorizon = 24 * time.Hour // 发货截止前 24 小时仍未交运 → 告警
	RelayStall      = 24 * time.Hour // 中转点签收后 24 小时未交运
)

// ScanLimit 单轮扫描上限：命中上限说明积压超出单轮处理能力，记警告（不许静默截断）。
const ScanLimit = 500

// Enqueuer 入队（接口放使用方；*queue.Client 满足，测试用假实现）。
// 签名与 queue.Client.Enqueue 完全一致（含可变选项）——否则不满足接口。
type Enqueuer interface {
	Enqueue(ctx context.Context, t queue.Task, opts ...asynq.Option) error
}

// Service 订单域服务。
type Service struct {
	repo    *Repo
	exc     *Exceptions
	shops   *store.Repo
	creds   *store.CredentialService
	factory PostingSourceFactory
	q       Enqueuer
	now     func() time.Time
}

// NewService 构造。factory 可为 nil（S1-B 合并后在装配层接上，拉单任务会明确报错）。
func NewService(repo *Repo, exc *Exceptions, shops *store.Repo, creds *store.CredentialService,
	factory PostingSourceFactory, q Enqueuer) *Service {
	return &Service{repo: repo, exc: exc, shops: shops, creds: creds, factory: factory, q: q, now: time.Now}
}

// Repo 暴露数据读写（purchase / shipment 用）。
func (s *Service) Repo() *Repo { return s.repo }

// Exceptions 暴露异常池（purchase / shipment 用）。
func (s *Service) Exceptions() *Exceptions { return s.exc }

// Shop 读店铺（purchase 取默认中转点、shipment 取店铺配置用）。
func (s *Service) Shop(ctx context.Context, storeID string) (*store.Shop, error) {
	return s.shops.ByID(ctx, storeID)
}

// Advance 推进订单内部状态（只前进不后退）。推进到 at_relay 时投「关采购任务」。
// shipment 的签收、purchase 的采购进度都调它。
func (s *Service) Advance(ctx context.Context, orderID, target string) (bool, error) {
	changed, err := s.repo.AdvanceStatus(ctx, orderID, target)
	if err != nil || !changed {
		return changed, err
	}
	if target == StatusAtRelay && s.q != nil {
		if err := s.q.Enqueue(ctx, CloseTask(orderID)); err != nil {
			// 投不出不算签收失败：补投扫描（purchase 侧按订单状态兜底）会补。
			logger.Errorf("[order] 投递关采购任务失败 order=%s: %v", orderID, err)
		}
	}
	return true, nil
}

// ---- 任务构造 ----

// PollTask 单店拉单任务。
func PollTask(storeID string, window time.Time) queue.Task {
	payload, _ := json.Marshal(map[string]string{"store_id": storeID})
	return queue.Task{
		Type:           TaskTypePoll,
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("order:poll:%s:%d", storeID, window.Unix()/int64(PollIntervalNoPush.Seconds())),
	}
}

// PlanTask 生成采购任务（投给 purchase）。
func PlanTask(orderID string) queue.Task {
	payload, _ := json.Marshal(map[string]string{"order_id": orderID})
	return queue.Task{Type: TaskTypePurchasePlan, Payload: payload, IdempotencyKey: "purchase:plan:" + orderID}
}

// CloseTask 关采购任务（投给 purchase）。
func CloseTask(orderID string) queue.Task {
	payload, _ := json.Marshal(map[string]string{"order_id": orderID})
	return queue.Task{Type: TaskTypePurchaseClose, Payload: payload, IdempotencyKey: "purchase:close:" + orderID}
}

// ---- 拉单 ----

// HandlePollStores 定时入口：找出到点该拉的店，按店投拉单任务（每店独立重试与幂等）。
func (s *Service) HandlePollStores(ctx context.Context, _ *asynq.Task) error {
	shops, err := s.shops.List(ctx)
	if err != nil {
		return fmt.Errorf("列店铺失败: %w", err)
	}
	now := s.now()
	due := 0
	for i := range shops {
		shop := &shops[i]
		if shop.Status != store.ShopStatusActive {
			continue
		}
		if shop.Mode == store.ModeFBP {
			continue // FBP 接入在 S2（总纲 §13.1）：S1 不为它拉单
		}
		if !s.due(shop, now) {
			continue
		}
		if err := s.q.Enqueue(ctx, PollTask(shop.ID, now)); err != nil {
			logger.Errorf("[order] 投递拉单任务失败 store=%s: %v", shop.Name, err)
			continue
		}
		due++
	}
	logger.L().Info("order poll sweep", "due", due, "shops", len(shops))
	return nil
}

// due 该店是否到点：开推送的店 30 分钟对账一次，其余 5 分钟。
func (s *Service) due(shop *store.Shop, now time.Time) bool {
	interval := PollIntervalNoPush
	if shop.PushEnabled {
		interval = PollIntervalPush
	}
	if shop.LastSyncAt == nil {
		return true
	}
	return now.Sub(*shop.LastSyncAt) >= interval
}

// HandlePoll 单店拉单任务入口。
func (s *Service) HandlePoll(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		StoreID string `json:"store_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("拉单任务载荷损坏: %w", err)
	}
	return s.PollStore(ctx, payload.StoreID)
}

// PollStore 拉一家店：时间窗拉单 + 未完成单兜底 → 幂等入库 → 按映射表执行动作 →
// 成功写回 stores.last_sync_at（总纲 §6）。
func (s *Service) PollStore(ctx context.Context, storeID string) error {
	shop, err := s.shops.ByID(ctx, storeID)
	if err != nil {
		return fmt.Errorf("读店铺失败: %w", err)
	}
	if shop.Status != store.ShopStatusActive {
		return nil
	}
	if s.factory == nil {
		return errors.New("未接入 Ozon 客户端（S1-B 合并后由装配层接上）")
	}
	cred, err := s.creds.Get(ctx, shop.ID, store.KindOzonAPIKey)
	if err != nil {
		return fmt.Errorf("读店铺 %s 的 Ozon 凭据失败: %w", shop.Name, err)
	}
	src, err := s.factory(*shop, cred)
	if err != nil {
		return fmt.Errorf("构造店铺 %s 的 Ozon 客户端失败: %w", shop.Name, err)
	}

	now := s.now()
	from := now.Add(-firstPollLookback)
	if shop.LastSyncAt != nil {
		from = shop.LastSyncAt.Add(-pollOverlap)
	}

	postings, err := src.ListPostings(ctx, ListPostingsRequest{Since: from, To: now})
	if err != nil {
		return fmt.Errorf("拉单失败 store=%s: %w", shop.Name, err)
	}
	// 未完成单兜底：老单的状态变化（尤其「平台未放行 → awaiting_packaging」）靠它捞回来。
	// ⚠️ 它失败就整轮失败：若照常写 last_sync_at，时间窗会一路前移，
	// 那个「早就建好、这轮才变 awaiting_packaging」的订单再不会被任何窗口捞回（重审 #9）。
	unfulfilled, err := src.ListUnfulfilled(ctx, ListPostingsRequest{To: now})
	if err != nil {
		return fmt.Errorf("拉未完成单失败 store=%s（本轮不写 last_sync_at，窗口下轮重扫）: %w", shop.Name, err)
	}
	postings = append(postings, unfulfilled...)

	seen := map[string]bool{}
	applied := 0
	for _, p := range postings {
		if p.PostingNumber == "" || seen[p.PostingNumber] {
			continue
		}
		seen[p.PostingNumber] = true
		if err := s.applyPosting(ctx, shop, src, p); err != nil {
			// 单条坏不拖垮整轮：记日志继续，下轮再来。
			logger.Errorf("[order] 处理 posting 失败 store=%s posting=%s: %v", shop.Name, p.PostingNumber, err)
			continue
		}
		applied++
	}

	if err := s.repo.MarkSynced(ctx, shop.ID, now); err != nil {
		return fmt.Errorf("写 last_sync_at 失败 store=%s: %w", shop.Name, err)
	}
	logger.L().Info("order polled", "store", shop.Name, "fetched", len(seen), "applied", applied)
	return nil
}

// applyPosting 单条 posting 入库 + 按映射表执行动作（总纲 §5.2）。
func (s *Service) applyPosting(ctx context.Context, shop *store.Shop, src PostingSource, p Posting) error {
	existing, err := s.repo.ByPosting(ctx, shop.ID, p.PostingNumber)
	isNew := errors.Is(err, ErrOrderNotFound)
	if err != nil && !isNew {
		return err
	}
	// 拉详情的两种情况：① 新单；② 老单但库里还没有商品行（首次拉详情失败过，
	// 更新路径不会补，订单会永远卡在「没有商品行」——重审 #4 的衍生项）。
	needDetail := isNew
	if !needDetail {
		if items, ierr := s.repo.Items(ctx, existing.ID); ierr == nil && len(items) == 0 {
			needDetail = true
		}
	}
	if needDetail {
		// 列表接口的商品行可能不全，以权威详情为准；拉不到就用列表数据兜。
		if detail, derr := src.GetPosting(ctx, p.PostingNumber); derr == nil && detail != nil {
			p = *detail
		} else if derr != nil {
			logger.Errorf("[order] 拉 posting 详情失败 store=%s posting=%s: %v", shop.Name, p.PostingNumber, derr)
		}
	}

	ord, created, err := s.repo.UpsertFromOzon(ctx, shop.ID, p)
	if err != nil {
		return err
	}
	if len(p.Items) > 0 {
		if err := s.repo.UpsertItems(ctx, ord.ID, p.Items); err != nil {
			return err
		}
	}

	rule := RuleFor(p.Status)
	if rule.ExceptionCode != "" {
		detail := fmt.Sprintf("posting=%s ozon_status=%s substatus=%s", p.PostingNumber, p.Status, p.Substatus)
		if err := s.exc.Raise(ctx, RefOrder, ord.ID, rule.ExceptionCode, detail); err != nil {
			logger.Errorf("[order] 写异常失败 order=%s code=%s: %v", ord.ID, rule.ExceptionCode, err)
		}
	}
	if rule.ForwardStatus != "" {
		if _, err := s.Advance(ctx, ord.ID, rule.ForwardStatus); err != nil {
			return err
		}
	}
	if created {
		logger.L().Info("order created", "store", shop.Name, "posting", p.PostingNumber, "ozon_status", p.Status)
	}

	// 允许采购且还没动过（new）：投采购计划任务（幂等键 = 订单，重复入队自动去重）。
	if rule.AllowedToPurchase && ord.Status == StatusNew {
		if err := s.q.Enqueue(ctx, PlanTask(ord.ID)); err != nil {
			return fmt.Errorf("投递采购计划任务失败 order=%s: %w", ord.ID, err)
		}
	}
	return nil
}

// ---- 超时扫描 ----

// HandleSweep 超时异常扫描（定时任务入口）。
func (s *Service) HandleSweep(ctx context.Context, _ *asynq.Task) error {
	now := s.now()
	swept := 0

	// 超时未采购：允许采购却没推进（new）或采购中卡住。
	rows, err := s.repo.TimeoutForPurchase(ctx, now.Add(-TimeoutPurchase))
	if err != nil {
		logger.Errorf("[order] 扫超时未采购失败: %v", err)
	} else {
		s.warnIfCapped("超时未采购", len(rows))
		for i := range rows {
			o := &rows[i]
			detail := fmt.Sprintf("posting=%s status=%s ozon_status=%s 超过 %s 未推进",
				o.PostingNumber, o.Status, o.OzonStatus, TimeoutPurchase)
			if err := s.exc.Raise(ctx, RefOrder, o.ID, CodePurchaseTimeout, detail); err != nil {
				logger.Errorf("[order] 写超时未采购异常失败 order=%s: %v", o.ID, err)
			} else {
				swept++
			}
		}
	}

	// 发货截止临近。
	rows, err = s.repo.DeadlineNear(ctx, now.Add(DeadlineHorizon))
	if err != nil {
		logger.Errorf("[order] 扫发货截止临近失败: %v", err)
	} else {
		s.warnIfCapped("发货截止临近", len(rows))
		for i := range rows {
			o := &rows[i]
			detail := fmt.Sprintf("posting=%s 发货截止 %s，当前状态 %s",
				o.PostingNumber, o.ShipDeadline.UTC().Format(time.RFC3339), o.Status)
			if err := s.exc.Raise(ctx, RefOrder, o.ID, CodeShipDeadlineNear, detail); err != nil {
				logger.Errorf("[order] 写发货截止临近异常失败 order=%s: %v", o.ID, err)
			} else {
				swept++
			}
		}
	}

	// 中转点停滞：签收后长时间没交运。
	rows, err = s.repo.RelayStalled(ctx, now.Add(-RelayStall))
	if err != nil {
		logger.Errorf("[order] 扫中转点停滞失败: %v", err)
	} else {
		s.warnIfCapped("中转点停滞", len(rows))
		for i := range rows {
			o := &rows[i]
			detail := fmt.Sprintf("posting=%s 中转点签收后超过 %s 未交运", o.PostingNumber, RelayStall)
			if err := s.exc.Raise(ctx, RefOrder, o.ID, CodeRelayStalled, detail); err != nil {
				logger.Errorf("[order] 写中转点停滞异常失败 order=%s: %v", o.ID, err)
			} else {
				swept++
			}
		}
	}

	logger.L().Info("order sweep done", "raised", swept)
	return nil
}

// warnIfCapped 命中单轮扫描上限时吭一声（不许静默截断）。
func (s *Service) warnIfCapped(name string, n int) {
	if n >= ScanLimit {
		logger.Warnf("[order] %s 命中扫描上限 %d 条，本轮只处理这些（积压超出单轮能力）", name, ScanLimit)
	}
}

// RescanRule 补投规则：停在「允许采购但没动」的订单重新投采购计划
// （写库与入队不在同一事务，总纲 §5.5）。
func (s *Service) RescanRule() queue.RescanRule {
	return queue.RescanRule{
		Name:    "order:plan",
		Timeout: PollIntervalNoPush,
		Rescan: func(ctx context.Context, cutoff time.Time) ([]queue.Task, error) {
			rows, err := s.repo.StuckForPurchase(ctx, cutoff)
			if err != nil {
				return nil, err
			}
			if len(rows) >= ScanLimit {
				logger.Warnf("[order] 采购计划补投命中扫描上限 %d 条", ScanLimit)
			}
			tasks := make([]queue.Task, 0, len(rows))
			for i := range rows {
				tasks = append(tasks, PlanTask(rows[i].ID))
			}
			return tasks, nil
		},
	}
}
