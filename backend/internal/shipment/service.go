// 发货域服务：签收（→ at_relay）、备货（复核 substatus）、取面单、
// 按 tpl_integration_type 传单号、货代仓交接对照表导出（总纲 §5.7、§7.4）。
package shipment

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/purchase"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
)

// Service 发货域服务。
type Service struct {
	repo      *Repo
	orders    *order.Service
	purchases *purchase.Repo
	creds     *store.CredentialService
	fulfiller FulfillerFactory
	audit     *audit.Recorder
	now       func() time.Time
}

// NewService 构造。fulfiller 可为 nil（S1-B 合并后由装配层接上）。
func NewService(repo *Repo, orders *order.Service, purchases *purchase.Repo,
	creds *store.CredentialService, fulfiller FulfillerFactory, rec *audit.Recorder) *Service {
	return &Service{repo: repo, orders: orders, purchases: purchases, creds: creds,
		fulfiller: fulfiller, audit: rec, now: time.Now}
}

// ListFilter 打包交接列表筛选。
type ListFilter struct {
	StoreID      string
	RelayPointID string
	Statuses     []string
	Page         int
	PageSize     int
}

// 打包页默认看的订单状态：货在往中转点走、或已在打包台上。
var packingStatuses = []string{order.StatusPurchased, order.StatusInbound, order.StatusAtRelay, order.StatusHandedOver}

// List 打包交接列表（订单 + 发运记录 + 国内段单号）。
func (s *Service) List(ctx context.Context, f ListFilter) ([]ListItem, int64, error) {
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = packingStatuses
	}
	orders, total, err := s.orders.Repo().List(ctx, order.OrderFilter{
		StoreID:      f.StoreID,
		RelayPointID: f.RelayPointID,
		Statuses:     statuses,
		Page:         f.Page,
		PageSize:     f.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(orders))
	for i := range orders {
		ids = append(ids, orders[i].ID)
	}
	shipments, err := s.repo.ByOrderIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	tracking, err := s.purchases.DomesticTrackingByOrder(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	items := make([]ListItem, 0, len(orders))
	for i := range orders {
		o := &orders[i]
		action, _ := order.TrackingActionFor(deref(o.TplIntegrationType))
		it := ListItem{
			OrderID:            o.ID,
			StoreID:            o.StoreID,
			PostingNumber:      o.PostingNumber,
			OrderStatus:        o.Status,
			TplIntegrationType: o.TplIntegrationType,
			ShipDeadline:       o.ShipDeadline,
			RelayPointID:       o.RelayPointID,
			TrackingAction:     trackingActionName(action),
		}
		if t, ok := tracking[o.ID]; ok {
			it.DomesticCarrier = t.Carrier
			it.DomesticTrackingNo = t.TrackingNo
		}
		if sh, ok := shipments[o.ID]; ok {
			it.Shipment = sh
		}
		items = append(items, it)
	}
	return items, total, nil
}

var (
	// ErrClientUnavailable Ozon 客户端未接入（S1-B 合并前）。
	ErrClientUnavailable = errors.New("未接入 Ozon 客户端（S1-B 合并后由装配层接上）")
)

// Receive 中转点签收（货代回传或自有仓扫码）→ 订单 at_relay（总纲 §5.7）。
func (s *Service) Receive(ctx context.Context, orderID string) (*Shipment, error) {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.IsTerminal(o.Status) {
		return nil, fmt.Errorf("订单已「%s」，不能再签收", o.Status)
	}
	row, err := s.repo.GetOrCreate(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.orders.Advance(ctx, o.ID, order.StatusAtRelay); err != nil {
		return nil, err
	}
	// Advance → at_relay 时会投「关采购任务」（在 order 包里统一做）。
	s.audit.Record(ctx, audit.Entry{
		Action: "shipment.receive", Object: "shipment:" + row.ID,
		Detail: mustJSON(map[string]string{"order_id": o.ID, "posting_number": o.PostingNumber}),
	})
	return row, nil
}

// ShipResult 备货结果。
type ShipResult struct {
	OrderStatus    string   `json:"order_status"`
	TrackingAction string   `json:"tracking_action"`
	Notes          []string `json:"notes"`
}

// Ship 备货：调 Ozon ship → 复核 substatus → 推进 handed_over。
// trackingNo / carrier 是国际段单号（只有 tpl 要求「由卖家登记」时才会用到）。
func (s *Service) Ship(ctx context.Context, orderID, trackingNo, carrier string) (*ShipResult, error) {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.IsTerminal(o.Status) {
		return nil, fmt.Errorf("订单已「%s」，不能备货", o.Status)
	}
	shop, err := s.orders.Shop(ctx, o.StoreID)
	if err != nil {
		return nil, err
	}
	// 备货时机：默认货到中转点之后；中转点要提前拿面单的店按 stores.ship_early 放宽（总纲 §13.1）。
	if !shop.ShipEarly && order.Rank(o.Status) < order.Rank(order.StatusAtRelay) {
		return nil, fmt.Errorf("订单还没到中转点（当前 %s）；如该店需要提前备货，把店铺的 ship_early 打开", o.Status)
	}
	fl, err := s.fulfillerFor(ctx, *shop)
	if err != nil {
		return nil, err
	}

	if err := fl.ShipPosting(ctx, o.PostingNumber); err != nil {
		return nil, fmt.Errorf("备货失败: %w", err)
	}
	// ⚠️ 返回 200 不代表成功：复核 substatus（总纲 §5.2）。
	if st, gerr := fl.GetPosting(ctx, o.PostingNumber); gerr == nil && st != nil {
		if st.Substatus == SubstatusShipFailed {
			s.raise(ctx, o, order.CodeShipFailed, fmt.Sprintf("备货返回成功但 substatus=%s", st.Substatus))
			return nil, fmt.Errorf("备货未生效：substatus=%s，已进异常池，请人工处理", st.Substatus)
		}
	} else if gerr != nil {
		logger.Errorf("[shipment] 复核 substatus 失败 posting=%s: %v", o.PostingNumber, gerr)
	}

	row, err := s.repo.GetOrCreate(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if err := s.repo.Update(ctx, &Shipment{ID: row.ID, HandedOverAt: &now}); err != nil {
		return nil, err
	}
	if _, err := s.orders.Advance(ctx, o.ID, order.StatusHandedOver); err != nil {
		return nil, err
	}

	res := &ShipResult{OrderStatus: order.StatusHandedOver}
	action, code := order.TrackingActionFor(deref(o.TplIntegrationType))
	res.TrackingAction = trackingActionName(action)
	switch action {
	case order.TrackingNone:
		// 单号由 Ozon 生成（备货后约 30 分钟）：我们只读不传，S3 拉轨迹时再回填。
		src := TrackingSourceOzon
		if err := s.repo.Update(ctx, &Shipment{ID: row.ID, TrackingSource: &src}); err != nil {
			logger.Errorf("[shipment] 记 tracking_source 失败 shipment=%s: %v", row.ID, err)
		}
	case order.TrackingSet:
		res.Notes = append(res.Notes, "该物流类型需要卖家登记单号：请调 /api/shipments/:id/tracking 传单号")
	case order.TrackingBlock:
		s.raise(ctx, o, code, fmt.Sprintf("tpl_integration_type=%s：S1 不处理该物流类型的单号", deref(o.TplIntegrationType)))
		res.Notes = append(res.Notes, "该物流类型的单号需人工处理，已进异常池")
	}

	if trackingNo != "" && action == order.TrackingSet {
		if err := s.setTracking(ctx, fl, o, row, trackingNo, carrier); err != nil {
			return nil, err
		}
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "shipment.ship", Object: "shipment:" + row.ID,
		Detail: mustJSON(map[string]string{"order_id": o.ID, "posting_number": o.PostingNumber, "tracking_action": res.TrackingAction}),
	})
	return res, nil
}

// SetTracking 单独传单号（操作台「传单号」入口；3pl_tracking / non_integrated 才允许）。
func (s *Service) SetTracking(ctx context.Context, orderID, trackingNo, carrier string) (*Shipment, error) {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	action, code := order.TrackingActionFor(deref(o.TplIntegrationType))
	switch action {
	case order.TrackingNone:
		return nil, fmt.Errorf("tpl_integration_type=%s：单号由 Ozon 生成，我们只读不传（总纲 §7.4）", deref(o.TplIntegrationType))
	case order.TrackingBlock:
		s.raise(ctx, o, code, fmt.Sprintf("tpl_integration_type=%s：S1 不处理该物流类型的单号", deref(o.TplIntegrationType)))
		return nil, fmt.Errorf("tpl_integration_type=%s 的单号需要人工处理，已进异常池", deref(o.TplIntegrationType))
	}
	shop, err := s.orders.Shop(ctx, o.StoreID)
	if err != nil {
		return nil, err
	}
	fl, err := s.fulfillerFor(ctx, *shop)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.GetOrCreate(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if err := s.setTracking(ctx, fl, o, row, trackingNo, carrier); err != nil {
		return nil, err
	}
	return s.repo.ByOrder(ctx, o.ID)
}

func (s *Service) setTracking(ctx context.Context, fl PostingFulfiller, o *order.Order, row *Shipment, trackingNo, carrier string) error {
	if trackingNo == "" {
		return errors.New("该物流类型需要卖家登记单号：tracking_no 不能为空")
	}
	if err := fl.SetTrackingNumber(ctx, o.PostingNumber, trackingNo, carrier); err != nil {
		return fmt.Errorf("传单号失败: %w", err)
	}
	src := TrackingSourceSeller
	ship := &Shipment{ID: row.ID, TrackingNo: &trackingNo, TrackingSource: &src}
	if carrier != "" {
		ship.Carrier = &carrier
	}
	if err := s.repo.Update(ctx, ship); err != nil {
		return err
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "shipment.set_tracking", Object: "shipment:" + row.ID,
		Detail: mustJSON(map[string]string{"order_id": o.ID, "tracking_no": trackingNo, "carrier": carrier}),
	})
	return nil
}

// Label 取面单 PDF（自有仓扫码出面单：按需从 Ozon 拉，不落盘）。
func (s *Service) Label(ctx context.Context, orderID string) ([]byte, string, error) {
	o, err := s.orders.Repo().ByID(ctx, orderID)
	if err != nil {
		return nil, "", err
	}
	shop, err := s.orders.Shop(ctx, o.StoreID)
	if err != nil {
		return nil, "", err
	}
	fl, err := s.fulfillerFor(ctx, *shop)
	if err != nil {
		return nil, "", err
	}
	pdf, err := fl.GetPackageLabel(ctx, o.PostingNumber)
	if err != nil {
		return nil, "", fmt.Errorf("取面单失败: %w", err)
	}
	// 面单引用记在发运记录上（S1 不落盘，取用即拉；S3 物流深化时改落盘）。
	ref := "ozon:package-label:" + o.PostingNumber
	if row, rerr := s.repo.GetOrCreate(ctx, o.ID); rerr == nil {
		if err := s.repo.Update(ctx, &Shipment{ID: row.ID, LabelRef: &ref}); err != nil {
			logger.Errorf("[shipment] 记面单引用失败 order=%s: %v", o.ID, err)
		}
	}
	filename := fmt.Sprintf("%s-label.pdf", o.PostingNumber)
	return pdf, filename, nil
}

// HandoverCSV 货代仓交接对照表：国内快递号 ↔ posting_number ↔ 面单（总纲 §5.7）。
// 翻页取全：单页上限 200，超出的静默截断会让货代少收几十单（重审 #6）。
func (s *Service) HandoverCSV(ctx context.Context, relayPointID string) ([]byte, error) {
	const pageSize = 200
	var items []ListItem
	for page := 1; ; page++ {
		rows, total, err := s.List(ctx, ListFilter{RelayPointID: relayPointID, Page: page, PageSize: pageSize})
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
		if len(rows) == 0 || len(items) >= int(total) {
			break
		}
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM：让 Excel 正确识别 UTF-8
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"posting_number", "国内快递号", "国内承运商", "订单状态", "发货截止", "物流类型", "传单号动作", "面单引用", "面单下载"})
	for _, it := range items {
		deadline := ""
		if it.ShipDeadline != nil {
			deadline = it.ShipDeadline.UTC().Format(time.RFC3339)
		}
		tpl := ""
		if it.TplIntegrationType != nil {
			tpl = *it.TplIntegrationType
		}
		labelRef, labelURL := "", ""
		if it.Shipment != nil && it.Shipment.LabelRef != nil {
			labelRef = *it.Shipment.LabelRef
		}
		labelURL = "/api/shipments/" + it.OrderID + "/label"
		_ = w.Write([]string{
			it.PostingNumber, it.DomesticTrackingNo, it.DomesticCarrier,
			it.OrderStatus, deadline, tpl, it.TrackingAction, labelRef, labelURL,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Service) fulfillerFor(ctx context.Context, shop store.Shop) (PostingFulfiller, error) {
	if s.fulfiller == nil {
		return nil, ErrClientUnavailable
	}
	cred, err := s.creds.Get(ctx, shop.ID, store.KindOzonAPIKey)
	if err != nil {
		return nil, fmt.Errorf("读店铺 %s 的 Ozon 凭据失败: %w", shop.Name, err)
	}
	return s.fulfiller(shop, cred)
}

func (s *Service) raise(ctx context.Context, o *order.Order, code, detail string) {
	if code == "" {
		return
	}
	if err := s.orders.Exceptions().Raise(ctx, order.RefOrder, o.ID, code, detail); err != nil {
		logger.Errorf("[shipment] 写异常失败 order=%s code=%s: %v", o.ID, code, err)
	}
}

func trackingActionName(a order.TrackingAction) string {
	switch a {
	case order.TrackingNone:
		return "none"
	case order.TrackingSet:
		return "set"
	default:
		return "block"
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
