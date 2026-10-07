// 采购域服务：新订单 → 采购任务（plan）、自动执行（execute，含下单防重）、
// 中转点签收后关单（close）、国内段停滞扫描（sweep）、补投规则。
package purchase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/catalog"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/queue"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/hibiken/asynq"
)

// 任务类型。
const (
	TaskTypeExecute = "purchase:execute" // 执行自动任务
	TaskTypeClose   = "purchase:close"   // 中转点签收后关单
	TaskTypeSweep   = "purchase:sweep"   // 国内段停滞扫描
)

// DomesticStall 国内段停滞阈值（子 spec §6 自定细节：下单后 72 小时无更新）。
const DomesticStall = 72 * time.Hour

// rescanTimeout 任务停在待处理多久算卡住（写库与入队不同事务的兜底，总纲 §5.5）。
const rescanTimeout = 5 * time.Minute

// Service 采购域服务。
type Service struct {
	repo   *Repo
	orders *order.Service
	relays *order.RelayRepo
	cat    *catalog.Repo
	creds  *store.CredentialService
	buyers BuyerClientFactory
	audit  *audit.Recorder
	q      order.Enqueuer
	now    func() time.Time
}

// NewService 构造。buyers 可为 nil（S1-C 合并后在装配层接上）。
func NewService(repo *Repo, orders *order.Service, relays *order.RelayRepo, cat *catalog.Repo,
	creds *store.CredentialService, buyers BuyerClientFactory, rec *audit.Recorder, q order.Enqueuer) *Service {
	return &Service{repo: repo, orders: orders, relays: relays, cat: cat, creds: creds,
		buyers: buyers, audit: rec, q: q, now: time.Now}
}

// Repo 暴露数据读写（shipment 的交接对照表取国内段单号用）。
func (s *Service) Repo() *Repo { return s.repo }

// ---- 任务构造 ----

// ExecuteTask 执行任务。
func ExecuteTask(taskID string) queue.Task {
	payload, _ := json.Marshal(map[string]string{"task_id": taskID})
	return queue.Task{Type: TaskTypeExecute, Payload: payload, IdempotencyKey: "purchase:execute:" + taskID}
}

// ---- plan：新订单 → 采购任务 ----

// HandlePlan 采购计划任务入口（由 order 轮询投递）。
func (s *Service) HandlePlan(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("采购计划任务载荷损坏: %w", err)
	}
	return s.PlanOrder(ctx, payload.OrderID)
}

// PlanOrder 为一张订单生成采购任务（幂等：已有任务就只推进订单状态）。
func (s *Service) PlanOrder(ctx context.Context, orderID string) error {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if errors.Is(err, order.ErrOrderNotFound) {
		return nil // 订单没了（测试库清过 / 被删）：静默结束
	}
	if err != nil {
		return err
	}
	if o.Status != order.StatusNew {
		return nil // 已推进过：幂等
	}
	if !order.RuleFor(o.OzonStatus).AllowedToPurchase {
		return nil // 平台未放行：等轮询看到 awaiting_packaging 再投
	}

	items, err := s.orders.Repo().Items(ctx, o.ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		s.raise(ctx, order.RefOrder, o.ID, order.CodeOfferMappingMissing, "订单没有商品行，无法生成采购任务")
		return nil
	}
	relay, err := s.relayFor(ctx, o)
	if err != nil {
		// 收货地址 = 中转点（总纲 §5.7）：没有可用中转点就是地址校验失败。
		s.raise(ctx, order.RefOrder, o.ID, order.CodeAddressInvalid, err.Error())
		return nil
	}

	existing, err := s.repo.TasksByOrder(ctx, o.ID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		created := s.createTasks(ctx, o, relay, items)
		if created == 0 {
			return nil // 全部商品没映射：异常已记，订单留在 new（超时扫描会再报）
		}
	}
	_, err = s.orders.Advance(ctx, o.ID, order.StatusPurchasing)
	return err
}

// createTasks 按货源商品分组建任务（一单可拆多任务：多供应商 / 多平台）。
func (s *Service) createTasks(ctx context.Context, o *order.Order, relay *order.RelayPoint, items []order.OrderItem) int {
	type group struct {
		offer *catalog.SupplierOffer
		items []TaskPayloadItem
	}
	groups := map[string]*group{}
	for i := range items {
		it := &items[i]
		link, offer, err := s.cat.ResolvePrimary(ctx, o.StoreID, it.OzonOfferID)
		if err != nil {
			s.raise(ctx, order.RefOrder, o.ID, order.CodeOfferMappingMissing,
				fmt.Sprintf("ozon_offer_id=%s 没有可用的货源映射", it.OzonOfferID))
			continue
		}
		if it.OfferLinkID == nil || *it.OfferLinkID != link.ID {
			if err := s.orders.Repo().SetItemOfferLink(ctx, it.ID, link.ID); err != nil {
				logger.Errorf("[purchase] 回填 order_item.offer_link_id 失败 item=%s: %v", it.ID, err)
			}
		}
		g := groups[offer.ID]
		if g == nil {
			g = &group{offer: offer}
			groups[offer.ID] = g
		}
		g.items = append(g.items, TaskPayloadItem{
			OzonOfferID: it.OzonOfferID,
			ItemID:      offer.ItemID,
			SkuID:       offer.SkuID,
			URL:         offer.URL,
			Qty:         it.Qty,
			UnitPrice:   offer.PurchasePrice, // 下单当时的价格快照（总纲 §5.3）
			Currency:    offer.Currency,
		})
	}

	created := 0
	for offerID, g := range groups {
		payload := TaskPayload{
			StoreID:       o.StoreID,
			PostingNumber: o.PostingNumber,
			RelayPointID:  relay.ID,
			RelayName:     relay.Name,
			RelayAddress:  relay.Address,
			RelayContact:  relay.Contact,
			ShipDeadline:  o.ShipDeadline,
			Currency:      g.offer.Currency,
			Items:         g.items,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			logger.Errorf("[purchase] 序列化任务载荷失败 order=%s: %v", o.ID, err)
			continue
		}
		idem := fmt.Sprintf("order:%s:offer:%s", o.ID, offerID)
		task := &PurchaseTask{
			OrderID:         o.ID,
			SupplierOfferID: &offerID,
			Channel:         g.offer.OrderChannel,
			ExecutorType:    g.offer.ExecutorType(),
			Status:          StatusPending,
			Payload:         raw,
			Deadline:        o.ShipDeadline,
			IdempotencyKey:  &idem,
		}
		ok, existing, err := s.repo.CreateTask(ctx, task)
		if err != nil {
			logger.Errorf("[purchase] 创建采购任务失败 order=%s offer=%s: %v", o.ID, offerID, err)
			continue
		}
		if !ok {
			_ = existing // 已存在：幂等，什么都不做
			continue
		}
		created++
		s.audit.Record(ctx, audit.Entry{
			Action: "purchase_task.create",
			Object: "purchase_task:" + task.ID,
			Detail: mustJSON(map[string]any{
				"order_id": o.ID, "supplier_offer_id": offerID,
				"channel": g.offer.OrderChannel, "executor_type": task.ExecutorType,
			}),
		})
		// 自动任务立即入队执行；人工任务等操作台（双轨，R8）。
		if task.ExecutorType == ExecutorAuto && s.q != nil {
			if err := s.q.Enqueue(ctx, ExecuteTask(task.ID)); err != nil {
				// 投不出不算失败：补投扫描会补（总纲 §5.5）。
				logger.Errorf("[purchase] 投递执行任务失败 task=%s: %v", task.ID, err)
			}
		}
	}
	return created
}

// relayFor 订单的中转点：订单上指定的优先，其次店铺默认；都没有 = 地址校验失败。
// 收货地址 = 中转点（总纲 §5.7），所以这里同时兜住「地址为空」的情况。
func (s *Service) relayFor(ctx context.Context, o *order.Order) (*order.RelayPoint, error) {
	if o.RelayPointID == nil || *o.RelayPointID == "" {
		shop, err := s.orders.Shop(ctx, o.StoreID)
		if err != nil {
			return nil, fmt.Errorf("读店铺失败: %w", err)
		}
		if shop.DefaultRelayPointID == nil || *shop.DefaultRelayPointID == "" {
			return nil, fmt.Errorf("订单与店铺 %s 都没有指定中转点（下单收货地址 = 中转点）", shop.Name)
		}
		if _, err := s.orders.Repo().SetRelayPoint(ctx, o.ID, *shop.DefaultRelayPointID); err != nil {
			return nil, err
		}
		o.RelayPointID = shop.DefaultRelayPointID
	}
	p, err := s.relays.ByID(ctx, *o.RelayPointID)
	if err != nil {
		if errors.Is(err, order.ErrRelayPointNotFound) {
			return nil, fmt.Errorf("订单指定的中转点不存在或已删除")
		}
		return nil, err
	}
	if p.Status != order.RelayStatusActive {
		return nil, fmt.Errorf("中转点 %s 已停用", p.Name)
	}
	if strings.TrimSpace(p.Address) == "" {
		return nil, fmt.Errorf("中转点 %s 没有填地址", p.Name)
	}
	return p, nil
}

// ---- execute：自动执行器 ----

// HandleExecute 执行任务入口。
func (s *Service) HandleExecute(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("执行任务载荷损坏: %w", err)
	}
	return s.Execute(ctx, payload.TaskID)
}

// Execute 执行一个自动采购任务（总纲 §5.1 的完整防重口径）。
func (s *Service) Execute(ctx context.Context, taskID string) error {
	task, err := s.repo.TaskByID(ctx, taskID)
	if errors.Is(err, ErrTaskNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status != StatusPending && task.Status != StatusExecuting {
		return nil // 已推进 / 已关单 / 异常：幂等结束
	}
	if task.ExecutorType != ExecutorAuto || task.Channel != catalog.ChannelSelfUse {
		return nil // 人工任务与跨境通道（S2）不走自动执行器
	}
	o, err := s.orders.Repo().ByID(ctx, task.OrderID)
	if errors.Is(err, order.ErrOrderNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	pay, perr := task.DecodePayload()
	if perr != nil {
		if err := s.repo.SetStatus(ctx, task.ID, StatusException); err != nil {
			logger.Errorf("[purchase] 置异常状态失败 task=%s: %v", task.ID, err)
		}
		s.raise(ctx, order.RefPurchaseTask, task.ID, order.CodeAlibabaOrderFailed, "任务载荷损坏: "+perr.Error())
		return nil
	}

	// 1) 先落 executing（下单前的第一道防重闸）。
	if task.Status == StatusPending {
		ok, err := s.repo.MarkExecuting(ctx, task.ID)
		if err != nil {
			return err
		}
		if !ok {
			return nil // 别的 worker 抢先了
		}
	}

	buyer, err := s.buyer(ctx)
	if err != nil {
		return s.retryOrFail(ctx, task, err)
	}

	// 2) 下单前核对：先查 1688 买家订单（崩溃重启后也走这里）——
	//    查到就补记、**不再下单**（总纲 §5.1 下单防重）。
	found, err := s.findPlacedOrder(ctx, buyer, task, o)
	if err != nil {
		return s.retryOrFail(ctx, task, fmt.Errorf("核对买家订单失败: %w", err))
	}
	if found != nil {
		return s.recordOrdered(ctx, task, found, "核对补记：1688 已有该 posting 的买家订单，不再下单")
	}

	// 3) 预览：新商家 / 地址问题在这里分流。
	addr := Address{Name: pay.RelayName, Phone: contactPhone(pay.RelayContact), Address: pay.RelayAddress}
	req := PreviewRequest{
		ItemID:    firstItem(pay).ItemID,
		SkuID:     firstItem(pay).SkuID,
		Qty:       totalQty(pay),
		UnitPrice: firstItem(pay).UnitPrice,
		Address:   addr,
	}
	pv, err := buyer.PreviewOrder(ctx, req)
	if err != nil {
		return s.retryOrFail(ctx, task, fmt.Errorf("下单预览失败: %w", err))
	}
	if pv != nil && !pv.CanOrder {
		switch pv.Reason {
		case PreviewReasonNewSeller:
			return s.convertToManual(ctx, task, "新商家首单：1688 买家自用版只能对下过单的老商家下单，自动转人工")
		case PreviewReasonAddressInvalid:
			s.raise(ctx, order.RefPurchaseTask, task.ID, order.CodeAddressInvalid, pv.Message)
			return s.repo.SetStatus(ctx, task.ID, StatusException)
		default:
			return s.retryOrFail(ctx, task, fmt.Errorf("下单预览未通过: %s", pv.Message))
		}
	}

	// 4) 下单。买家留言带 posting_number（中转点认包 + 防重核对都靠它）。
	bo, err := buyer.CreateOrder(ctx, CreateOrderRequest{
		ItemID:    req.ItemID,
		SkuID:     req.SkuID,
		Qty:       req.Qty,
		UnitPrice: req.UnitPrice,
		Address:   addr,
		Remark:    o.PostingNumber,
	})
	if err != nil {
		return s.retryOrFail(ctx, task, fmt.Errorf("1688 下单失败: %w", err))
	}
	return s.recordOrdered(ctx, task, bo, "自动下单成功")
}

// findPlacedOrder 查买家订单里是否已有本 posting 的单（按买家留言里的 posting_number 认）。
func (s *Service) findPlacedOrder(ctx context.Context, buyer BuyerClient, task *PurchaseTask, o *order.Order) (*BuyerOrder, error) {
	since := task.CreatedAt.Add(-time.Hour) // 往前留一小时余量（时钟与入库延迟）
	list, err := buyer.ListBuyerOrders(ctx, ListBuyerOrdersRequest{Since: since, To: s.now()})
	if err != nil {
		return nil, err
	}
	for i := range list {
		if o.PostingNumber != "" && strings.Contains(list[i].Remark, o.PostingNumber) {
			return &list[i], nil
		}
	}
	return nil, nil
}

// recordOrdered 记下单结果：采购单入库 + 任务 ordered + 订单状态回推。
func (s *Service) recordOrdered(ctx context.Context, task *PurchaseTask, bo *BuyerOrder, note string) error {
	if bo == nil {
		return s.retryOrFail(ctx, task, errors.New("下单结果为空"))
	}
	po := &PurchaseOrder{
		TaskID:             task.ID,
		PlatformOrderID:    bo.PlatformOrderID,
		Amount:             bo.Amount,
		Currency:           currencyOr(bo.Currency),
		DomesticCarrier:    nullable(bo.Carrier),
		DomesticTrackingNo: nullable(bo.TrackingNo),
	}
	if err := s.repo.UpsertPurchaseOrder(ctx, po); err != nil {
		return fmt.Errorf("记采购单失败: %w", err)
	}
	if err := s.repo.SetStatus(ctx, task.ID, StatusOrdered); err != nil {
		return err
	}
	s.syncOrderStatus(ctx, task.OrderID)
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.ordered",
		Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]any{"platform_order_id": bo.PlatformOrderID, "amount": bo.Amount.String(), "note": note}),
	})
	logger.L().Info("purchase task ordered", "task", task.ID, "platform_order_id", bo.PlatformOrderID, "note", note)
	return nil
}

// retryOrFail 失败处理：还有重试次数就让 asynq 退避重试；用尽则进异常池。
func (s *Service) retryOrFail(ctx context.Context, task *PurchaseTask, cause error) error {
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	if retried >= maxRetry {
		if err := s.repo.SetStatus(ctx, task.ID, StatusException); err != nil {
			logger.Errorf("[purchase] 置异常状态失败 task=%s: %v", task.ID, err)
		}
		s.raise(ctx, order.RefPurchaseTask, task.ID, order.CodeAlibabaOrderFailed, cause.Error())
		return nil
	}
	return cause
}

// convertToManual 自动转人工（新商家首单，总纲 §5.1）。
func (s *Service) convertToManual(ctx context.Context, task *PurchaseTask, reason string) error {
	pay, _ := task.DecodePayload()
	pay.Note = reason
	raw, err := json.Marshal(pay)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateFields(ctx, task.ID, map[string]any{
		"executor_type": ExecutorManual,
		"status":        StatusPending,
		"payload":       raw,
	}); err != nil {
		return err
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.convert_manual", Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]string{"reason": reason}),
	})
	logger.L().Info("purchase task converted to manual", "task", task.ID, "reason", reason)
	return nil
}

// ConvertManual 操作台手动转人工。
func (s *Service) ConvertManual(ctx context.Context, taskID, assignee, note string) (*PurchaseTask, error) {
	task, err := s.repo.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status == StatusClosed || task.Status == StatusShipped || task.Status == StatusPaid {
		return nil, errors.New("任务已执行完成，不能再转人工")
	}
	pay, _ := task.DecodePayload()
	if note != "" {
		pay.Note = note
	}
	raw, _ := json.Marshal(pay)
	fields := map[string]any{
		"executor_type": ExecutorManual,
		"payload":       raw,
	}
	if task.Status == StatusExecuting || task.Status == StatusException {
		fields["status"] = StatusPending
	}
	if assignee != "" {
		fields["assignee"] = assignee
	}
	if err := s.repo.UpdateFields(ctx, task.ID, fields); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.convert_manual", Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]string{"assignee": assignee, "note": note}),
	})
	return s.repo.TaskByID(ctx, task.ID)
}

// ---- close：中转点签收后关单 ----

// HandleClose 关单任务入口。
func (s *Service) HandleClose(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("关单任务载荷损坏: %w", err)
	}
	return s.CloseOrder(ctx, payload.OrderID)
}

// CloseOrder 订单已签收（at_relay 及以后）→ 该订单的采购任务关单（总纲 §5.1 状态机终点）。
func (s *Service) CloseOrder(ctx context.Context, orderID string) error {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if errors.Is(err, order.ErrOrderNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Rank(o.Status) < order.Rank(order.StatusAtRelay) {
		return nil // 还没签收：等补投扫描下轮再看
	}
	tasks, err := s.repo.TasksByOrder(ctx, orderID)
	if err != nil {
		return err
	}
	for i := range tasks {
		task := &tasks[i]
		switch task.Status {
		case StatusOrdered, StatusPaid, StatusShipped:
			if err := s.repo.SetStatus(ctx, task.ID, StatusClosed); err != nil {
				logger.Errorf("[purchase] 关单失败 task=%s: %v", task.ID, err)
				continue
			}
			s.audit.Record(ctx, audit.Entry{
				Action: "purchase_task.closed", Object: "purchase_task:" + task.ID,
				Detail: mustJSON(map[string]string{"order_id": orderID, "reason": "中转点已签收"}),
			})
		}
	}
	s.syncOrderStatus(ctx, orderID)
	return nil
}

// ---- 状态回推与扫描 ----

// syncOrderStatus 采购任务状态 → 订单状态（只前进）：
// 最不推进的任务决定订单走到哪（一单多任务时别越过没做完的）。
func (s *Service) syncOrderStatus(ctx context.Context, orderID string) {
	tasks, err := s.repo.TasksByOrder(ctx, orderID)
	if err != nil || len(tasks) == 0 {
		return
	}
	minStat := ""
	minRank := -1
	for _, t := range tasks {
		var cur string
		switch t.Status {
		case StatusPending, StatusExecuting:
			cur = order.StatusPurchasing
		case StatusOrdered, StatusPaid:
			cur = order.StatusPurchased
		case StatusShipped:
			cur = order.StatusInbound
		case StatusClosed:
			cur = order.StatusAtRelay
		default:
			continue // exception：不参与回推，交给异常池
		}
		if r := order.Rank(cur); minRank == -1 || r < minRank {
			minRank = r
			minStat = cur
		}
	}
	if minRank == -1 {
		return
	}
	if _, err := s.orders.Advance(ctx, orderID, minStat); err != nil {
		logger.Errorf("[purchase] 回推订单状态失败 order=%s: %v", orderID, err)
	}
}

// HandleSweep 国内段停滞扫描（定时任务入口）。
func (s *Service) HandleSweep(ctx context.Context, _ *asynq.Task) error {
	rows, err := s.repo.TasksForRescan(ctx, []string{StatusShipped}, s.now().Add(-DomesticStall))
	if err != nil {
		return fmt.Errorf("扫国内段停滞失败: %w", err)
	}
	for i := range rows {
		task := &rows[i]
		detail := fmt.Sprintf("采购任务 %s 已发国内段超过 %s 无更新", task.ID, DomesticStall)
		if err := s.raiseErr(ctx, order.RefPurchaseTask, task.ID, order.CodeDomesticStalled, detail); err != nil {
			logger.Errorf("[purchase] 写国内段停滞异常失败 task=%s: %v", task.ID, err)
		}
	}
	logger.L().Info("purchase sweep done", "stalled", len(rows))
	return nil
}

// RescanRules 补投规则（总纲 §5.5：写库与入队不同事务的兜底）。
func (s *Service) RescanRules() []queue.RescanRule {
	return []queue.RescanRule{
		{
			Name:    "purchase:execute",
			Timeout: rescanTimeout,
			Rescan: func(ctx context.Context, cutoff time.Time) ([]queue.Task, error) {
				rows, err := s.repo.TasksForRescan(ctx, []string{StatusPending, StatusExecuting}, cutoff)
				if err != nil {
					return nil, err
				}
				tasks := make([]queue.Task, 0, len(rows))
				for i := range rows {
					if rows[i].ExecutorType != ExecutorAuto {
						continue // 人工任务在等人，不是卡住
					}
					tasks = append(tasks, ExecuteTask(rows[i].ID))
				}
				return tasks, nil
			},
		},
		{
			Name:    "purchase:close",
			Timeout: 10 * time.Minute,
			Rescan: func(ctx context.Context, cutoff time.Time) ([]queue.Task, error) {
				rows, err := s.repo.TasksForClose(ctx, cutoff)
				if err != nil {
					return nil, err
				}
				seen := map[string]bool{}
				tasks := make([]queue.Task, 0, len(rows))
				for i := range rows {
					if seen[rows[i].OrderID] {
						continue
					}
					seen[rows[i].OrderID] = true
					tasks = append(tasks, order.CloseTask(rows[i].OrderID))
				}
				return tasks, nil
			},
		},
	}
}

// ---- 小工具 ----

// buyer 造 1688 客户端（企业级凭据：应用密钥 + 买家 token）。
func (s *Service) buyer(ctx context.Context) (BuyerClient, error) {
	if s.buyers == nil {
		return nil, errors.New("1688 客户端未接入（S1-C 合并后由装配层接上）")
	}
	token, err := s.creds.Get(ctx, "", store.KindAlibabaToken)
	if err != nil {
		return nil, fmt.Errorf("读 1688 买家 token 失败: %w", err)
	}
	app, err := s.creds.Get(ctx, "", store.KindAlibabaApp)
	if err != nil && !errors.Is(err, store.ErrCredentialNotFound) {
		return nil, fmt.Errorf("读 1688 应用密钥失败: %w", err)
	}
	return s.buyers(BuyerCredentials{App: app, Token: token})
}

func (s *Service) raise(ctx context.Context, refType, refID, code, detail string) {
	if err := s.raiseErr(ctx, refType, refID, code, detail); err != nil {
		logger.Errorf("[purchase] 写异常失败 ref=%s/%s code=%s: %v", refType, refID, code, err)
	}
}

func (s *Service) raiseErr(ctx context.Context, refType, refID, code, detail string) error {
	return s.orders.Exceptions().Raise(ctx, refType, refID, code, detail)
}

func firstItem(p TaskPayload) TaskPayloadItem {
	if len(p.Items) == 0 {
		return TaskPayloadItem{}
	}
	return p.Items[0]
}

func totalQty(p TaskPayload) int {
	n := 0
	for _, it := range p.Items {
		n += it.Qty
	}
	if n <= 0 {
		n = 1
	}
	return n
}

// contactPhone 中转点 contact 是自由文本（"张三 13800000000"），
// 下单接口要电话——取出其中的数字串，取不到就整串给过去（下游校验会拦）。
func contactPhone(contact string) string {
	var sb strings.Builder
	for _, r := range contact {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	if sb.Len() >= 6 {
		return sb.String()
	}
	return contact
}

func currencyOr(c string) string {
	if c == "" {
		return "CNY"
	}
	return c
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
