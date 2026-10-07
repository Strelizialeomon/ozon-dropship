// Package shipment 是「发货域」（总纲 §5.7、§7.4）：中转点、交接、签收、备货、
// 面单、按 tpl_integration_type 传单号。
//
// 依赖方向（ADR-20261007-go-package-deps）：shipment 依赖 order。
// 额外依赖 purchase：交接对照表要国内快递号（在 purchase_orders.domestic_tracking_no），
// 而 order 去读那张表会与 purchase → order 构成环——这条边是本份新增的，
// 已在 S1 父 issue #5 声明、PR 里单独说明（接口 settle：purchase.Repo.DomesticTrackingByOrder）。
package shipment

import (
	"encoding/json"
	"errors"
	"time"
)

// 国际段单号来源（总纲 §7.4：两段单号别混）。
const (
	TrackingSourceOzon   = "ozon"   // 单号由 Ozon 生成，我们只读
	TrackingSourceSeller = "seller" // 单号由我们传
)

// ErrShipmentNotFound 发运记录不存在。
var ErrShipmentNotFound = errors.New("发运记录不存在")

// Shipment 发运（表 shipments）。tracking_no 是**国际段**（中转点 → 买家）；
// 国内段单号在 purchase_orders 里，不回传 Ozon、也不进本表（总纲 §7.4）。
type Shipment struct {
	ID             string          `gorm:"primaryKey;type:varchar(32)" json:"id"`
	OrderID        string          `gorm:"column:order_id;type:varchar(32)" json:"order_id"`
	TrackingNo     *string         `gorm:"column:tracking_no;type:varchar(64)" json:"tracking_no"`
	TrackingSource *string         `gorm:"column:tracking_source;type:varchar(16)" json:"tracking_source"`
	Carrier        *string         `gorm:"type:varchar(64)" json:"carrier"`
	LabelRef       *string         `gorm:"column:label_ref;type:varchar(255)" json:"label_ref"`
	HandedOverAt   *time.Time      `gorm:"column:handed_over_at" json:"handed_over_at"`
	Events         json.RawMessage `gorm:"type:json" json:"events"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DelFlag        bool            `json:"del_flag"`
}

// TableName 显式表名。
func (Shipment) TableName() string { return "shipments" }

// ListItem 打包交接列表项（订单 + 发运记录 + 国内段单号）。
type ListItem struct {
	OrderID            string     `json:"order_id"`
	StoreID            string     `json:"store_id"`
	PostingNumber      string     `json:"posting_number"`
	OrderStatus        string     `json:"order_status"`
	TplIntegrationType *string    `json:"tpl_integration_type"`
	ShipDeadline       *time.Time `json:"ship_deadline"`
	RelayPointID       *string    `json:"relay_point_id"`
	DomesticCarrier    string     `json:"domestic_carrier"`
	DomesticTrackingNo string     `json:"domestic_tracking_no"`
	Shipment           *Shipment  `json:"shipment"`
	TrackingAction     string     `json:"tracking_action"` // none / set / block（按 tpl 判定）
}
