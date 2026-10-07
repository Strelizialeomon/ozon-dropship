// Package catalog 是「货源商品与按店映射」域（总纲 §5.3）：
// supplier_offers（货源商品：平台、商品 ID、规格、链接、采购价、境内运费、库存、下单通道）
// 与 offer_links（按店映射：store_id + ozon_offer_id ↔ 货源商品，按 priority 排主备）。
//
// 在依赖链里 catalog 在下（ADR-20261007-go-package-deps），只依赖 internal/infra，不 import 其他业务包。
package catalog

import (
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// 货源平台。
const (
	Platform1688   = "1688"
	PlatformPDD    = "pdd"
	PlatformTaobao = "taobao"
)

// 下单通道（总纲 §5.3）。S1 只在「自用版 / 人工」两条通道上下单；
// cross_border（跨境自用版）依赖 1688 白名单，S2 起接入（总纲 §12.2）。
const (
	ChannelSelfUse     = "self_use"     // 买家自用版：可自动下单
	ChannelCrossBorder = "cross_border" // 跨境自用版：S2 起
	ChannelManual      = "manual"       // 人工渠道（拼多多 / 淘宝）：出备料单
)

// 货源商品状态。
const (
	StatusActive     = "active"
	StatusOutOfStock = "out_of_stock"
	StatusInvalid    = "invalid"
)

// ErrOfferNotFound 货源商品不存在 / 已软删。
var ErrOfferNotFound = errors.New("货源商品不存在")

// ErrLinkNotFound 映射不存在 / 已软删。
var ErrLinkNotFound = errors.New("按店映射不存在")

// ErrInvalid 字段校验失败（handler 转 1001）。
type ErrInvalid struct{ Reason string }

func (e *ErrInvalid) Error() string { return e.Reason }

// SupplierOffer 货源商品（表 supplier_offers，总纲 §6）。
type SupplierOffer struct {
	ID                  string           `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Platform            string           `gorm:"type:varchar(16)" json:"platform"`
	ItemID              string           `gorm:"column:item_id;type:varchar(64)" json:"item_id"`
	SkuID               string           `gorm:"column:sku_id;type:varchar(64)" json:"sku_id"`
	URL                 string           `gorm:"type:varchar(512)" json:"url"`
	PurchasePrice       decimal.Decimal  `gorm:"type:decimal(18,4)" json:"purchase_price"`
	DomesticFreight     decimal.Decimal  `gorm:"column:domestic_freight;type:decimal(18,4)" json:"domestic_freight"`
	Currency            string           `gorm:"type:char(3)" json:"currency"`
	Stock               int              `json:"stock"`
	PriceAlertThreshold *decimal.Decimal `gorm:"column:price_alert_threshold;type:decimal(6,4)" json:"price_alert_threshold"`
	OrderChannel        string           `gorm:"column:order_channel;type:varchar(16)" json:"order_channel"`
	Followed            bool             `json:"followed"`
	Status              string           `gorm:"type:varchar(16)" json:"status"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	DelFlag             bool             `json:"del_flag"`
}

// TableName 显式表名（本项目约定：表名复数、模型显式声明）。
func (SupplierOffer) TableName() string { return "supplier_offers" }

// OfferLink 按店映射（表 offer_links，总纲 §5.3）。
// 唯一键 store_id + ozon_offer_id + supplier_offer_id（仅存活行）。
type OfferLink struct {
	ID              string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	StoreID         string    `gorm:"column:store_id;type:varchar(32)" json:"store_id"`
	OzonOfferID     string    `gorm:"column:ozon_offer_id;type:varchar(128)" json:"ozon_offer_id"`
	SupplierOfferID string    `gorm:"column:supplier_offer_id;type:varchar(32)" json:"supplier_offer_id"`
	Priority        int       `gorm:"default:1" json:"priority"` // 数字小 = 主货源
	TargetStock     int       `gorm:"column:target_stock" json:"target_stock"`
	LastPushedStock *int      `gorm:"column:last_pushed_stock" json:"last_pushed_stock"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	DelFlag         bool      `json:"del_flag"`
}

// TableName 显式表名。
func (OfferLink) TableName() string { return "offer_links" }

// validateOffer 校验货源商品字段（S1 的开单通道限制也在这里拦）。
func validateOffer(o *SupplierOffer) error {
	switch o.Platform {
	case Platform1688, PlatformPDD, PlatformTaobao:
	default:
		return &ErrInvalid{Reason: "platform 只能是 1688 / pdd / taobao"}
	}
	if strings.TrimSpace(o.ItemID) == "" {
		return &ErrInvalid{Reason: "item_id 不能为空"}
	}
	switch o.OrderChannel {
	case ChannelSelfUse, ChannelManual:
	case ChannelCrossBorder:
		return &ErrInvalid{Reason: "跨境自用版（cross_border）通道 S2 起接入，S1 只支持 self_use / manual"}
	default:
		return &ErrInvalid{Reason: "order_channel 只能是 self_use / cross_border / manual"}
	}
	// 人工通道（拼多多 / 淘宝）没有下单 API（总纲 §3.3）：通道只能是 manual。
	if o.Platform != Platform1688 && o.OrderChannel != ChannelManual {
		return &ErrInvalid{Reason: "非 1688 货源只能走 manual 人工通道"}
	}
	if o.Currency == "" {
		o.Currency = "CNY"
	}
	if len(o.Currency) != 3 {
		return &ErrInvalid{Reason: "currency 必须是 3 位币种码"}
	}
	if o.PurchasePrice.IsNegative() || o.DomesticFreight.IsNegative() {
		return &ErrInvalid{Reason: "价格 / 运费不能为负"}
	}
	if o.PriceAlertThreshold != nil && (o.PriceAlertThreshold.IsNegative() || o.PriceAlertThreshold.GreaterThan(decimal.NewFromInt(1))) {
		return &ErrInvalid{Reason: "price_alert_threshold 是比例（0.1 = 10%），取值 (0,1]"}
	}
	if o.Status == "" {
		o.Status = StatusActive
	}
	switch o.Status {
	case StatusActive, StatusOutOfStock, StatusInvalid:
	default:
		return &ErrInvalid{Reason: "status 只能是 active / out_of_stock / invalid"}
	}
	return nil
}

// validateLink 校验映射。
func validateLink(l *OfferLink) error {
	if l.StoreID == "" {
		return &ErrInvalid{Reason: "store_id 不能为空"}
	}
	if strings.TrimSpace(l.OzonOfferID) == "" {
		return &ErrInvalid{Reason: "ozon_offer_id 不能为空"}
	}
	if l.SupplierOfferID == "" {
		return &ErrInvalid{Reason: "supplier_offer_id 不能为空"}
	}
	if l.Priority <= 0 {
		return &ErrInvalid{Reason: "priority 从 1 起（数字小 = 主货源）"}
	}
	if l.TargetStock < 0 {
		return &ErrInvalid{Reason: "target_stock 不能为负"}
	}
	return nil
}

// ExecutorType 通道 → 采购任务执行器类型：self_use 可自动，其余人工（总纲 §5.1）。
// 新商家首单的「自动转人工」不在这里定——那要等 1688 下单预览的结论（§5.1）。
func (o *SupplierOffer) ExecutorType() string {
	if o.OrderChannel == ChannelSelfUse {
		return "auto"
	}
	return "manual"
}
