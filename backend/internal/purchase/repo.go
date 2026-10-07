// 采购任务与采购单的数据读写。
package purchase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repo 数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ---- 采购任务 ----

// TaskByID 按 ID 取。
func (r *Repo) TaskByID(ctx context.Context, id string) (*PurchaseTask, error) {
	var t PurchaseTask
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TasksByOrder 某订单的全部采购任务。
func (r *Repo) TasksByOrder(ctx context.Context, orderID string) ([]PurchaseTask, error) {
	var rows []PurchaseTask
	err := r.db.WithContext(ctx).
		Where("order_id = ? AND del_flag = ?", orderID, false).
		Order("created_at").Find(&rows).Error
	return rows, err
}

// CreateTask 新建（幂等键撞唯一键时返回 false + 现有任务）。
func (r *Repo) CreateTask(ctx context.Context, t *PurchaseTask) (bool, *PurchaseTask, error) {
	if t.ID == "" {
		t.ID = snowflake.GenStringID()
	}
	err := r.db.WithContext(ctx).Create(t).Error
	if err == nil {
		return true, t, nil
	}
	if isDuplicate(err) {
		var existing PurchaseTask
		if key := deref(t.IdempotencyKey); key != "" {
			if e := r.db.WithContext(ctx).Where("idempotency_key = ? AND del_flag = ?", key, false).
				First(&existing).Error; e == nil {
				return false, &existing, nil
			}
		}
		return false, nil, err
	}
	return false, nil, err
}

// MarkExecuting pending → executing 的 CAS（同一任务并发执行只有一个能过）。
func (r *Repo) MarkExecuting(ctx context.Context, taskID string) (bool, error) {
	return r.SetStatusFrom(ctx, taskID, []string{StatusPending}, StatusExecuting)
}

// SetStatusFrom 带前置状态的 CAS：只有任务当前处在 from 之一才改，返回是否真改了。
// 人工动作（回填 / 记已付款）与自动执行并发时，没有这道闸就会互相覆盖、状态还会倒退
// （PR #16 重审 #7）。
func (r *Repo) SetStatusFrom(ctx context.Context, taskID string, from []string, to string) (bool, error) {
	if len(from) == 0 {
		return false, errors.New("CAS 缺少前置状态")
	}
	res := r.db.WithContext(ctx).Model(&PurchaseTask{}).
		Where("id = ? AND status IN ? AND del_flag = ?", taskID, from, false).
		Updates(map[string]any{"status": to, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// SetStatus 无条件置状态：只给测试/脚本用，业务路径一律走 SetStatusFrom（CAS）。
func (r *Repo) SetStatus(ctx context.Context, taskID, status string) error {
	return r.db.WithContext(ctx).Model(&PurchaseTask{}).Where("id = ?", taskID).
		Updates(map[string]any{"status": status, "updated_at": time.Now().UTC()}).Error
}

// UpdateFields 更新任务的执行器 / 负责人 / 载荷 / 截止时间。
func (r *Repo) UpdateFields(ctx context.Context, taskID string, fields map[string]any) error {
	fields["updated_at"] = time.Now().UTC()
	return r.db.WithContext(ctx).Model(&PurchaseTask{}).Where("id = ?", taskID).Updates(fields).Error
}

// TaskFilter 任务台筛选。
type TaskFilter struct {
	OrderID      string
	StoreID      string
	Status       string
	Statuses     []string
	ExecutorType string
	Assignee     string
	Page         int
	PageSize     int
}

// TaskListItem 任务台列表项（任务 + 订单摘要）。
type TaskListItem struct {
	PurchaseTask
	PostingNumber string `json:"posting_number"`
	StoreID       string `json:"store_id"`
	OrderStatus   string `json:"order_status"`
}

// ListTasks 分页列表（带订单摘要：posting_number / store_id / 订单状态）。
func (r *Repo) ListTasks(ctx context.Context, f TaskFilter) ([]TaskListItem, int64, error) {
	q := r.db.WithContext(ctx).Table("purchase_tasks AS t").
		Joins("LEFT JOIN orders o ON o.id = t.order_id").
		Where("t.del_flag = ?", false)
	if f.OrderID != "" {
		q = q.Where("t.order_id = ?", f.OrderID)
	}
	if f.StoreID != "" {
		q = q.Where("o.store_id = ?", f.StoreID)
	}
	if f.Status != "" {
		q = q.Where("t.status = ?", f.Status)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("t.status IN ?", f.Statuses)
	}
	if f.ExecutorType != "" {
		q = q.Where("t.executor_type = ?", f.ExecutorType)
	}
	if f.Assignee != "" {
		q = q.Where("t.assignee = ?", f.Assignee)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(f.Page, f.PageSize)
	var rows []TaskListItem
	err := q.Select("t.*, o.posting_number AS posting_number, o.store_id AS store_id, o.status AS order_status").
		Order("t.created_at DESC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, err
}

// TasksForRescan 停在某状态且超时的任务（补投：执行 / 关单）。
func (r *Repo) TasksForRescan(ctx context.Context, statuses []string, cutoff time.Time) ([]PurchaseTask, error) {
	var rows []PurchaseTask
	err := r.db.WithContext(ctx).
		Where("del_flag = ? AND status IN ? AND updated_at < ?", false, statuses, cutoff).
		Order("updated_at").Limit(500).Find(&rows).Error
	return rows, err
}

// TasksForClose 该关单的任务（补投用）：
//   - 订单已签收（at_relay 及以后）+ 任务在 ordered/paid/shipped → 走完流程该关；
//   - 订单已取消 / 退货 + 任务还在 pending/executing → 不会再采购了，也收掉（重审 #5）。
func (r *Repo) TasksForClose(ctx context.Context, cutoff time.Time) ([]PurchaseTask, error) {
	var rows []PurchaseTask
	err := r.db.WithContext(ctx).Table("purchase_tasks AS t").
		Joins("JOIN orders o ON o.id = t.order_id").
		Where(`t.del_flag = ? AND t.updated_at < ? AND (
			(t.status IN ? AND o.status IN ?) OR
			(t.status IN ? AND o.status IN ?))`,
			false, cutoff,
			[]string{StatusOrdered, StatusPaid, StatusShipped},
			[]string{"at_relay", "handed_over", "in_transit", "delivered", "completed"},
			[]string{StatusPending, StatusExecuting},
			[]string{"cancelled", "returned"}).
		Select("t.*").Limit(500).Scan(&rows).Error
	return rows, err
}

// ---- 采购单 ----

// PurchaseOrderByTask 某任务的采购单（一个任务一张，重试时更新同一张）。
func (r *Repo) PurchaseOrderByTask(ctx context.Context, taskID string) (*PurchaseOrder, error) {
	var po PurchaseOrder
	err := r.db.WithContext(ctx).Where("task_id = ? AND del_flag = ?", taskID, false).First(&po).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &po, nil
}

// UpsertPurchaseOrder 记下单结果：一个任务一张采购单（唯一键 uk_purchase_orders_task，
// 见迁移 20261007130000），没有就建、有就按字段更新——并发写不会再写出两行（重审 #7）。
// 空值一律不覆盖已有值。overwritePlatformID 控制平台单号：
// 人工回填/纠错传 true；自动执行器的迟到结果传 false（不把人工填的号盖掉）。
func (r *Repo) UpsertPurchaseOrder(ctx context.Context, po *PurchaseOrder, overwritePlatformID bool) error {
	if po.ID == "" {
		po.ID = snowflake.GenStringID()
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if po.PlatformOrderID != "" {
		if overwritePlatformID {
			updates["platform_order_id"] = po.PlatformOrderID
		} else {
			// 已有非空单号就不动它（迟到的自动结果不许盖掉人工填的号）。
			updates["platform_order_id"] = gorm.Expr(
				"IF(platform_order_id = '' OR platform_order_id IS NULL, ?, platform_order_id)", po.PlatformOrderID)
		}
	}
	if !po.Amount.IsZero() {
		updates["amount"] = po.Amount
	}
	if po.Currency != "" {
		updates["currency"] = po.Currency
	}
	if po.PaidAt != nil {
		updates["paid_at"] = po.PaidAt
	}
	if po.DomesticCarrier != nil {
		updates["domestic_carrier"] = po.DomesticCarrier
	}
	if po.DomesticTrackingNo != nil {
		updates["domestic_tracking_no"] = po.DomesticTrackingNo
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(po).Error
}

// DomesticTrackingByOrder 国内段单号（交接对照表用）：订单 → 单号 / 承运商。
// 取该订单所有采购单里第一个有单号的（一单多任务时以先到的为准）。
func (r *Repo) DomesticTrackingByOrder(ctx context.Context, orderIDs []string) (map[string]DomesticTracking, error) {
	out := map[string]DomesticTracking{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	type row struct {
		OrderID    string
		Carrier    *string
		TrackingNo *string
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("purchase_orders AS po").
		Joins("JOIN purchase_tasks AS t ON t.id = po.task_id").
		Where("t.order_id IN ? AND po.del_flag = ? AND t.del_flag = ? AND po.domestic_tracking_no IS NOT NULL",
			orderIDs, false, false).
		Select("t.order_id AS order_id, po.domestic_carrier AS carrier, po.domestic_tracking_no AS tracking_no").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r0 := range rows {
		if _, ok := out[r0.OrderID]; ok {
			continue
		}
		out[r0.OrderID] = DomesticTracking{
			OrderID:    r0.OrderID,
			Carrier:    deref(r0.Carrier),
			TrackingNo: deref(r0.TrackingNo),
		}
	}
	return out, nil
}

// ---- 小工具 ----

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// isDuplicate 唯一键冲突（幂等键 alive_idem 生成列；与 store / catalog 同款兜底）。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "Duplicate entry")
}
