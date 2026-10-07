// 发运记录的数据读写。
package shipment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"

	"gorm.io/gorm"
)

// Repo 数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ByOrder 按订单取（一个订单一条）。
func (r *Repo) ByOrder(ctx context.Context, orderID string) (*Shipment, error) {
	var s Shipment
	err := r.db.WithContext(ctx).Where("order_id = ? AND del_flag = ?", orderID, false).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShipmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ByOrderIDs 批量取（列表拼装用）。
func (r *Repo) ByOrderIDs(ctx context.Context, orderIDs []string) (map[string]*Shipment, error) {
	out := map[string]*Shipment{}
	if len(orderIDs) == 0 {
		return out, nil
	}
	var rows []Shipment
	if err := r.db.WithContext(ctx).
		Where("order_id IN ? AND del_flag = ?", orderIDs, false).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].OrderID] = &rows[i]
	}
	return out, nil
}

// GetOrCreate 取订单的发运记录，没有就建（签收 / 首次打包动作时懒建）。
func (r *Repo) GetOrCreate(ctx context.Context, orderID string) (*Shipment, error) {
	existing, err := r.ByOrder(ctx, orderID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrShipmentNotFound) {
		return nil, err
	}
	row := &Shipment{ID: snowflake.GenStringID(), OrderID: orderID}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		if isDuplicate(err) {
			return r.ByOrder(ctx, orderID)
		}
		return nil, err
	}
	return row, nil
}

// Update 更新可改字段。
func (r *Repo) Update(ctx context.Context, s *Shipment) error {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if s.TrackingNo != nil {
		updates["tracking_no"] = s.TrackingNo
	}
	if s.TrackingSource != nil {
		updates["tracking_source"] = s.TrackingSource
	}
	if s.Carrier != nil {
		updates["carrier"] = s.Carrier
	}
	if s.LabelRef != nil {
		updates["label_ref"] = s.LabelRef
	}
	if s.HandedOverAt != nil {
		updates["handed_over_at"] = s.HandedOverAt
	}
	return r.db.WithContext(ctx).Model(&Shipment{}).Where("id = ?", s.ID).Updates(updates).Error
}

// isDuplicate 唯一键冲突（与 store / catalog / purchase 同款兜底）。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "Duplicate entry")
}
