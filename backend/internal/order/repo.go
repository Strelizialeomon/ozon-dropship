// 订单数据读写。
package order

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"gorm.io/gorm"
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
// 已存在的行**只更新 Ozon 侧字段**，绝不动内部状态、中转点、买家密文——
// 轮询每 5 分钟来一次，误写内部状态等于把人工推进的流程一脚踹回去。
// 返回 created = 本次是不是新建。
func (r *Repo) UpsertFromOzon(ctx context.Context, storeID string, p Posting) (*Order, bool, error) {
	now := time.Now().UTC()
	existing, err := r.ByPosting(ctx, storeID, p.PostingNumber)
	switch {
	case err == nil:
		updates := map[string]any{
			"order_number":          p.OrderNumber,
			"ozon_status":           p.Status,
			"ozon_substatus":        nullable(p.Substatus),
			"tpl_integration_type":  nullable(p.TplIntegrationType),
			"ship_deadline":         p.ShipDeadline,
			"parent_posting_number": nullable(p.ParentPostingNumber),
			"total_amount":          p.TotalAmount(),
			"currency":              currencyOr(p.Currency),
			"updated_at":            now,
		}
		if err := r.db.WithContext(ctx).Model(&Order{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return nil, false, err
		}
		existing.OrderNumber = p.OrderNumber
		existing.OzonStatus = p.Status
		existing.OzonSubstatus = nullable(p.Substatus)
		existing.TplIntegrationType = nullable(p.TplIntegrationType)
		existing.ShipDeadline = p.ShipDeadline
		existing.ParentPostingNumber = nullable(p.ParentPostingNumber)
		existing.TotalAmount = p.TotalAmount()
		existing.Currency = currencyOr(p.Currency)
		return existing, false, nil
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
		// 并发轮询撞唯一键：对方刚建好，转更新路径。
		if isDuplicate(err) {
			return r.UpsertFromOzon(ctx, storeID, p)
		}
		return nil, false, err
	}
	return o, true, nil
}

// UpsertItems 订单行按 ozon_offer_id 对齐：新的插入、已有的更新数量与价格
// （保留 offer_link_id——那是下游认过货源的结果，不能被轮询抹掉）。
func (r *Repo) UpsertItems(ctx context.Context, orderID string, items []PostingItem) error {
	if len(items) == 0 {
		return nil
	}
	existing, err := r.Items(ctx, orderID)
	if err != nil {
		return err
	}
	byOffer := make(map[string]*OrderItem, len(existing))
	for i := range existing {
		byOffer[existing[i].OzonOfferID] = &existing[i]
	}
	now := time.Now().UTC()
	for _, it := range items {
		cur := byOffer[it.OzonOfferID]
		if cur == nil {
			row := &OrderItem{
				ID:          snowflake.GenStringID(),
				OrderID:     orderID,
				OzonOfferID: it.OzonOfferID,
				Qty:         it.Qty,
				Price:       it.Price,
				Currency:    currencyOr(it.Currency),
			}
			if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
				return err
			}
			continue
		}
		if cur.Qty == it.Qty && cur.Price.Equal(it.Price) {
			continue
		}
		if err := r.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", cur.ID).
			Updates(map[string]any{
				"qty":        it.Qty,
				"price":      it.Price,
				"currency":   currencyOr(it.Currency),
				"updated_at": now,
			}).Error; err != nil {
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
		return true
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
