// 中转点模型与读写（表 relay_points，总纲 §5.7）。
//
// 模型放 order 的理由：收货地址是**下单参数**——orders.relay_point_id 指向它，
// purchase（下单 / 备料单要填地址）与 shipment（中转点 CRUD / 交接 / 签收）都要用；
// order 在依赖链上低于两者，谁都能往上取，不必新增 purchase ↔ shipment 的横边
// （ADR-20261007-go-package-deps）。
package order

import (
	"context"
	"errors"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"

	"gorm.io/gorm"
)

// 中转点类型（总纲 §5.7）。
const (
	RelayKindForwarder    = "forwarder"     // 货代 / 物流商代打包仓
	RelayKindOwnWarehouse = "own_warehouse" // 自有仓
)

// 中转点状态。
const (
	RelayStatusActive   = "active"
	RelayStatusDisabled = "disabled"
)

// ErrRelayPointNotFound 中转点不存在 / 已软删。
var ErrRelayPointNotFound = errors.New("中转点不存在")

// RelayPoint 中转点（表 relay_points）。
type RelayPoint struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Name      string    `gorm:"type:varchar(128)" json:"name"`
	Kind      string    `gorm:"type:varchar(16)" json:"kind"`
	Address   string    `gorm:"type:varchar(512)" json:"address"`
	Contact   string    `gorm:"type:varchar(128)" json:"contact"`
	Status    string    `gorm:"type:varchar(16)" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DelFlag   bool      `json:"del_flag"`
}

// TableName 显式表名。
func (RelayPoint) TableName() string { return "relay_points" }

// RelayRepo 中转点读写。
type RelayRepo struct{ db *gorm.DB }

// NewRelayRepo 构造。
func NewRelayRepo(db *gorm.DB) *RelayRepo { return &RelayRepo{db: db} }

// List 全部未删中转点。
func (r *RelayRepo) List(ctx context.Context) ([]RelayPoint, error) {
	var rows []RelayPoint
	err := r.db.WithContext(ctx).Where("del_flag = ?", false).Order("name").Find(&rows).Error
	return rows, err
}

// ByID 按 ID 取。
func (r *RelayRepo) ByID(ctx context.Context, id string) (*RelayPoint, error) {
	var p RelayPoint
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRelayPointNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create 新建。
func (r *RelayRepo) Create(ctx context.Context, p *RelayPoint) error {
	if p.ID == "" {
		p.ID = snowflake.GenStringID()
	}
	return r.db.WithContext(ctx).Create(p).Error
}

// Update 更新可改字段。
func (r *RelayRepo) Update(ctx context.Context, p *RelayPoint) error {
	res := r.db.WithContext(ctx).Model(&RelayPoint{}).
		Where("id = ? AND del_flag = ?", p.ID, false).
		Updates(map[string]any{
			"name":       p.Name,
			"kind":       p.Kind,
			"address":    p.Address,
			"contact":    p.Contact,
			"status":     p.Status,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRelayPointNotFound
	}
	return nil
}

// SoftDelete 软删。
func (r *RelayRepo) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&RelayPoint{}).Where("id = ?", id).
		Updates(map[string]any{"del_flag": true, "updated_at": time.Now().UTC()}).Error
}
