// 货源商品与按店映射的数据读写（本包自带，不设单独数据层）。
package catalog

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Repo 数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// OfferFilter 货源商品列表筛选。
type OfferFilter struct {
	Platform string
	Status   string
	Keyword  string // 按 item_id / sku_id / url 模糊匹配
}

// LinkFilter 映射列表筛选。
type LinkFilter struct {
	StoreID         string
	OzonOfferID     string
	SupplierOfferID string
}

// ---- 货源商品 ----

// ListOffers 列表（不含软删）。
func (r *Repo) ListOffers(ctx context.Context, f OfferFilter) ([]SupplierOffer, error) {
	q := r.db.WithContext(ctx).Where("del_flag = ?", false)
	if f.Platform != "" {
		q = q.Where("platform = ?", f.Platform)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("item_id LIKE ? OR sku_id LIKE ? OR url LIKE ?", like, like, like)
	}
	var rows []SupplierOffer
	err := q.Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// OfferByID 按 ID 取。
func (r *Repo) OfferByID(ctx context.Context, id string) (*SupplierOffer, error) {
	var o SupplierOffer
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOfferNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// CreateOffer 新建。
func (r *Repo) CreateOffer(ctx context.Context, o *SupplierOffer) error {
	return r.db.WithContext(ctx).Create(o).Error
}

// UpdateOffer 更新可改字段（带 del_flag=false 条件，0 行 = 不存在）。
func (r *Repo) UpdateOffer(ctx context.Context, o *SupplierOffer) error {
	res := r.db.WithContext(ctx).Model(&SupplierOffer{}).
		Where("id = ? AND del_flag = ?", o.ID, false).
		Updates(map[string]any{
			"platform":              o.Platform,
			"item_id":               o.ItemID,
			"sku_id":                o.SkuID,
			"url":                   o.URL,
			"purchase_price":        o.PurchasePrice,
			"domestic_freight":      o.DomesticFreight,
			"currency":              o.Currency,
			"stock":                 o.Stock,
			"price_alert_threshold": o.PriceAlertThreshold,
			"order_channel":         o.OrderChannel,
			"followed":              o.Followed,
			"status":                o.Status,
			"updated_at":            time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrOfferNotFound
	}
	return nil
}

// SoftDeleteOffer 软删。
func (r *Repo) SoftDeleteOffer(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&SupplierOffer{}).Where("id = ?", id).
		Updates(map[string]any{"del_flag": true, "updated_at": time.Now().UTC()}).Error
}

// ---- 按店映射 ----

// ListLinks 列表（不含软删）。
func (r *Repo) ListLinks(ctx context.Context, f LinkFilter) ([]OfferLink, error) {
	q := r.db.WithContext(ctx).Where("del_flag = ?", false)
	if f.StoreID != "" {
		q = q.Where("store_id = ?", f.StoreID)
	}
	if f.OzonOfferID != "" {
		q = q.Where("ozon_offer_id = ?", f.OzonOfferID)
	}
	if f.SupplierOfferID != "" {
		q = q.Where("supplier_offer_id = ?", f.SupplierOfferID)
	}
	var rows []OfferLink
	err := q.Order("store_id, ozon_offer_id, priority").Find(&rows).Error
	return rows, err
}

// LinkByID 按 ID 取。
func (r *Repo) LinkByID(ctx context.Context, id string) (*OfferLink, error) {
	var l OfferLink
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateLink 新建。
func (r *Repo) CreateLink(ctx context.Context, l *OfferLink) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// UpdateLink 更新 priority / target_stock（store+offer 组合是映射的身份，不做改）。
func (r *Repo) UpdateLink(ctx context.Context, l *OfferLink) error {
	res := r.db.WithContext(ctx).Model(&OfferLink{}).
		Where("id = ? AND del_flag = ?", l.ID, false).
		Updates(map[string]any{
			"priority":     l.Priority,
			"target_stock": l.TargetStock,
			"updated_at":   time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLinkNotFound
	}
	return nil
}

// SoftDeleteLink 软删。
func (r *Repo) SoftDeleteLink(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&OfferLink{}).Where("id = ?", id).
		Updates(map[string]any{"del_flag": true, "updated_at": time.Now().UTC()}).Error
}

// ErrNoUsableOffer 该（店铺 + Ozon 商品）没有可用的货源映射。
var ErrNoUsableOffer = &ErrInvalid{Reason: "该店铺该商品没有可用的货源映射"}

// ResolvePrimary 解析主货源：按 priority 从小到大（数字小 = 主货源）取第一个
// 「映射存活 + 货源商品存活且未失效」的组合。
// S1 不接库存同步（S2），故 out_of_stock 仍算可用；只有 invalid（商品失效）被跳过。
func (r *Repo) ResolvePrimary(ctx context.Context, storeID, ozonOfferID string) (*OfferLink, *SupplierOffer, error) {
	var links []OfferLink
	if err := r.db.WithContext(ctx).
		Where("store_id = ? AND ozon_offer_id = ? AND del_flag = ?", storeID, ozonOfferID, false).
		Order("priority").Find(&links).Error; err != nil {
		return nil, nil, err
	}
	if len(links) == 0 {
		return nil, nil, ErrNoUsableOffer
	}
	ids := make([]string, 0, len(links))
	for i := range links {
		ids = append(ids, links[i].SupplierOfferID)
	}
	var offers []SupplierOffer
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND del_flag = ?", ids, false).Find(&offers).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[string]*SupplierOffer, len(offers))
	for i := range offers {
		byID[offers[i].ID] = &offers[i]
	}
	for i := range links {
		o := byID[links[i].SupplierOfferID]
		if o == nil {
			continue // 货源被删：跳到下一优先
		}
		if o.Status == StatusInvalid {
			continue // 商品已失效（总纲 §5.10 的推送来源）：S1 静默跳过，库存同步 S2 接管
		}
		return &links[i], o, nil
	}
	return nil, nil, ErrNoUsableOffer
}
