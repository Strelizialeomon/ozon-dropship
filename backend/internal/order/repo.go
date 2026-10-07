// 订单数据读写。
package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repo 订单数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ByID 按 ID 取。
func (r *Repo) ByID(ctx context.Context, id string) (*Order, error) {
	var o Order
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ByPosting 按幂等键取（store_id + posting_number）。
func (r *Repo) ByPosting(ctx context.Context, storeID, postingNumber string) (*Order, error) {
	var o Order
	err := r.db.WithContext(ctx).
		Where("store_id = ? AND posting_number = ? AND del_flag = ?", storeID, postingNumber, false).
		First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// Items 订单行（按创建顺序）。
func (r *Repo) Items(ctx context.Context, orderID string) ([]OrderItem, error) {
	var rows []OrderItem
	err := r.db.WithContext(ctx).
		Where("order_id = ? AND del_flag = ?", orderID, false).
		Order("created_at").Find(&rows).Error
	return rows, err
}

// UpsertFromOzon 按 store_id + posting_number 幂等入库（总纲 §5.5）。
//
// 两条口径（PR #16 重审修订）：
//   - **只在真有变化时才写库**：orders.updated_at 是超时扫描（超时未采购 / 中转点停滞 /
//     补投）的计时器，每轮轮询无条件刷新会让这些判定永不触发；
//   - **空值不清空**：列表载荷可能缺字段（商品行不全、没有 shipment_date / tpl），
//     直接覆盖会把 total_amount 归零、把发货截止清成 NULL（告警从此不响）。
//
// 已存在的行只更新 Ozon 侧字段，绝不动内部状态、中转点、买家密文。
func (r *Repo) UpsertFromOzon(ctx context.Context, storeID string, p Posting) (*Order, bool, error) {
	existing, err := r.ByPosting(ctx, storeID, p.PostingNumber)
	switch {
	case err == nil:
		return r.applyOzonFields(ctx, existing, p, false)
	case !errors.Is(err, ErrOrderNotFound):
		return nil, false, err
	}

	// 新建：首次见到时若已在状态表后半段（如刚接系统时店里有在途单），按映射落对应内部状态。
	status := StatusNew
	if fwd := RuleFor(p.Status).ForwardStatus; fwd != "" {
		status = fwd
	}
	o := &Order{
		ID:                  snowflake.GenStringID(),
		StoreID:             storeID,
		PostingNumber:       p.PostingNumber,
		OrderNumber:         p.OrderNumber,
		ParentPostingNumber: nullable(p.ParentPostingNumber),
		Status:              status,
		OzonStatus:          p.Status,
		OzonSubstatus:       nullable(p.Substatus),
		TplIntegrationType:  nullable(p.TplIntegrationType),
		ShipDeadline:        p.ShipDeadline,
		TotalAmount:         p.TotalAmount(),
		Currency:            currencyOr(p.Currency),
	}
	if err := r.db.WithContext(ctx).Create(o).Error; err != nil {
		if isDuplicate(err) {
			// 并发：对方刚建好 → 重查一次转更新。
			// ⚠️ 只重试一次、不递归：若唯一键被「已软删的历史行」占着，查不到存活行，
			// 递归会一路撞键直到 goroutine 栈溢出（栈溢出不可 recover，整个进程会崩）。
			if again, qerr := r.ByPosting(ctx, storeID, p.PostingNumber); qerr == nil {
				return r.applyOzonFields(ctx, again, p, false)
			}
			return nil, false, fmt.Errorf("订单入库撞唯一键但查不到存活行（posting=%s 可能留有已软删的历史行）: %w", p.PostingNumber, err)
		}
		return nil, false, err
	}
	return o, true, nil
}

// applyOzonFields 把 Ozon 侧字段合并进已有订单：只写「有新值且确实变了」的那些列。
// created 参数只用于返回值签名统一（更新路径恒为 false）。
func (r *Repo) applyOzonFields(ctx context.Context, o *Order, p Posting, created bool) (*Order, bool, error) {
	updates := map[string]any{}
	now := time.Now().UTC()

	if v := p.OrderNumber; v != "" && v != o.OrderNumber {
		updates["order_number"] = v
		o.OrderNumber = v
	}
	if v := p.Status; v != "" && v != o.OzonStatus {
		updates["ozon_status"] = v
		o.OzonStatus = v
	}
	if v := p.Substatus; v != "" && deref(o.OzonSubstatus) != v {
		updates["ozon_substatus"] = v
		o.OzonSubstatus = &v
	}
	if v := p.TplIntegrationType; v != "" && deref(o.TplIntegrationType) != v {
		updates["tpl_integration_type"] = v
		o.TplIntegrationType = &v
	}
	if p.ShipDeadline != nil && (o.ShipDeadline == nil || !o.ShipDeadline.Equal(*p.ShipDeadline)) {
		updates["ship_deadline"] = p.ShipDeadline
		o.ShipDeadline = p.ShipDeadline
	}
	if v := p.ParentPostingNumber; v != "" && deref(o.ParentPostingNumber) != v {
		updates["parent_posting_number"] = v
		o.ParentPostingNumber = &v
	}
	// 商品行没带就不动金额与币种（列表接口可能不带行项目）。
	if len(p.Items) > 0 {
		if total := p.TotalAmount(); !total.Equal(o.TotalAmount) {
			updates["total_amount"] = total
			o.TotalAmount = total
		}
		if v := p.Currency; v != "" && v != o.Currency {
			updates["currency"] = v
			o.Currency = v
		}
	}
	if len(updates) == 0 {
		return o, created, nil // 没有任何变化：不写库、不刷新 updated_at（计时器保真）
	}
	updates["updated_at"] = now
	if err := r.db.WithContext(ctx).Model(&Order{}).Where("id = ?", o.ID).Updates(updates).Error; err != nil {
		return nil, false, err
	}
	return o, created, nil
}

// UpsertItems 订单行按 (order_id, ozon_offer_id) 幂等 upsert（唯一键见迁移 20261007130000）：
// 并发拉单也只会有一行——**绝不叠加数量**（重复行会让采购任务买双份）。
// offer_link_id 不在这里写（那是下游认过货源的结果，轮询不许抹掉）。
func (r *Repo) UpsertItems(ctx context.Context, orderID string, items []PostingItem) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, it := range items {
		row := &OrderItem{
			ID:          snowflake.GenStringID(),
			OrderID:     orderID,
			OzonOfferID: it.OzonOfferID,
			Qty:         it.Qty,
			Price:       it.Price,
			Currency:    currencyOr(it.Currency),
		}
		err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "order_id"}, {Name: "ozon_offer_id"}},
			DoUpdates: clause.Assignments(map[string]any{"qty": it.Qty, "price": it.Price, "currency": currencyOr(it.Currency), "updated_at": now}),
		}).Create(row).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// SetItemOfferLink 记录订单行认到的货源映射（purchase 生成采购任务时回填，事后可追溯）。
func (r *Repo) SetItemOfferLink(ctx context.Context, itemID, offerLinkID string) error {
	return r.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"offer_link_id": offerLinkID, "updated_at": time.Now().UTC()}).Error
}

// AdvanceStatus 推进内部状态（只前进不后退，总纲 §5.9）。
// 取消 / 退货是旁支终态：可以随时落，且落下之后不再被推进。
// 返回是否真的改了。
func (r *Repo) AdvanceStatus(ctx context.Context, orderID, target string) (bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		o, err := r.ByID(ctx, orderID)
		if err != nil {
			return false, err
		}
		if !shouldAdvance(o.Status, target) {
			return false, nil
		}
		res := r.db.WithContext(ctx).Model(&Order{}).
			Where("id = ? AND status = ?", orderID, o.Status).
			Updates(map[string]any{"status": target, "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return false, res.Error
		}
		if res.RowsAffected > 0 {
			return true, nil
		}
		// 0 行 = 并发改了状态：重读再判一次。
	}
	return false, errors.New("订单状态推进并发冲突（重试 3 次未成）")
}

// shouldAdvance 前进规则：终态不再动；取消类旁支直接落；主线只许 rank 变大。
func shouldAdvance(current, target string) bool {
	if current == target {
		return false
	}
	if IsTerminal(current) {
		return false
	}
	if target == StatusCancelled || target == StatusReturned {
		// 已送达 / 已完成之后不再被取消覆盖：货已经交付，后续属退货域（S3 的 returned 流程）。
		return Rank(current) < Rank(StatusDelivered)
	}
	return Rank(target) > Rank(current)
}

// SetRelayPoint 记订单的中转点（下单前定：店铺默认或单个订单改）。
func (r *Repo) SetRelayPoint(ctx context.Context, orderID, relayPointID string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&Order{}).
		Where("id = ? AND del_flag = ?", orderID, false).
		Updates(map[string]any{"relay_point_id": nullable(relayPointID), "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkSynced 写回 stores.last_sync_at（总纲 §6：S1-D 写、/api/system 读）。
// 店铺表归 store 包，这里用它的模型写这一列——总纲 §6 明写了写方是本份。
func (r *Repo) MarkSynced(ctx context.Context, storeID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&store.Shop{}).Where("id = ?", storeID).
		Update("last_sync_at", at.UTC()).Error
}

// OrderFilter 列表筛选。
type OrderFilter struct {
	StoreID      string
	RelayPointID string
	Statuses     []string
	OzonStatus   string
	Tpl          string
	Keyword      string // posting_number / order_number 模糊
	From         *time.Time
	To           *time.Time
	Page         int
	PageSize     int
}

// List 分页列表。
func (r *Repo) List(ctx context.Context, f OrderFilter) ([]Order, int64, error) {
	q := r.db.WithContext(ctx).Model(&Order{}).Where("del_flag = ?", false)
	if f.StoreID != "" {
		q = q.Where("store_id = ?", f.StoreID)
	}
	if f.RelayPointID != "" {
		q = q.Where("relay_point_id = ?", f.RelayPointID)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	}
	if f.OzonStatus != "" {
		q = q.Where("ozon_status = ?", f.OzonStatus)
	}
	if f.Tpl != "" {
		q = q.Where("tpl_integration_type = ?", f.Tpl)
	}
	if k := strings.TrimSpace(f.Keyword); k != "" {
		like := "%" + k + "%"
		q = q.Where("posting_number LIKE ? OR order_number LIKE ?", like, like)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", f.From.UTC())
	}
	if f.To != nil {
		q = q.Where("created_at <= ?", f.To.UTC())
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(f.Page, f.PageSize)
	var rows []Order
	err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// StatusesBelow 主线里 rank 小于 limit 的状态（发货截止临近扫描用）。
func StatusesBelow(limit string) []string {
	var out []string
	lim := Rank(limit)
	for st, rk := range statusRank {
		if rk < lim {
			out = append(out, st)
		}
	}
	return out
}

// ---- 扫描查询（超时 / 补投）----

// StuckForPurchase 停在「允许采购但还没动」的订单（补投 purchase:plan 用）。
func (r *Repo) StuckForPurchase(ctx context.Context, cutoff time.Time) ([]Order, error) {
	var rows []Order
	err := r.db.WithContext(ctx).
		Where("del_flag = ? AND status = ? AND ozon_status = ? AND updated_at < ?",
			false, StatusNew, OzonAwaitingPackaging, cutoff).
		Order("updated_at").Limit(500).Find(&rows).Error
	return rows, err
}

// TimeoutForPurchase 超时未采购：允许采购却没推进（new）或采购中（purchasing）卡住。
func (r *Repo) TimeoutForPurchase(ctx context.Context, cutoff time.Time) ([]Order, error) {
	var rows []Order
	err := r.db.WithContext(ctx).
		Where("del_flag = ? AND updated_at < ? AND ((status = ? AND ozon_status = ?) OR status = ?)",
			false, cutoff, StatusNew, OzonAwaitingPackaging, StatusPurchasing).
		Limit(500).Find(&rows).Error
	return rows, err
}

// DeadlineNear 发货截止临近：截止时间进窗口（horizon = now + 提前量）且还没交运。
func (r *Repo) DeadlineNear(ctx context.Context, horizon time.Time) ([]Order, error) {
	var rows []Order
	err := r.db.WithContext(ctx).
		Where("del_flag = ? AND ship_deadline IS NOT NULL AND ship_deadline <= ? AND status IN ?",
			false, horizon, StatusesBelow(StatusHandedOver)).
		Limit(500).Find(&rows).Error
	return rows, err
}

// RelayStalled 中转点停滞：签收后长时间没交运。
func (r *Repo) RelayStalled(ctx context.Context, cutoff time.Time) ([]Order, error) {
	var rows []Order
	err := r.db.WithContext(ctx).
		Where("del_flag = ? AND status = ? AND updated_at < ?", false, StatusAtRelay, cutoff).
		Limit(500).Find(&rows).Error
	return rows, err
}

// ---- 小工具 ----

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func currencyOr(c string) string {
	if c == "" {
		return "CNY"
	}
	return c
}
