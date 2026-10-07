// Package purchase 是「采购域」（总纲 §5.1、§7.3）：采购任务状态机、自动 / 人工两种执行器、
// 下单防重、备料单与回填。
//
// 依赖方向（ADR-20261007-go-package-deps）：purchase 依赖 order、catalog。
package purchase

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// 采购任务状态机（总纲 §5.1）：
// pending → executing → ordered → paid → shipped（货源已发国内段）→ closed（中转点已签收）；
// 任一步失败或超时 → exception。
const (
	StatusPending   = "pending"
	StatusExecuting = "executing"
	StatusOrdered   = "ordered"
	StatusPaid      = "paid"
	StatusShipped   = "shipped"
	StatusClosed    = "closed"
	StatusException = "exception"
)

// 执行器类型。
const (
	ExecutorAuto   = "auto"   // 1688 API 下单（买家自用版）
	ExecutorManual = "manual" // 人工执行：出备料单、人工下单后回填
)

// ErrTaskNotFound 采购任务不存在。
var ErrTaskNotFound = errors.New("采购任务不存在")

// PurchaseTask 采购任务（表 purchase_tasks）。一单可拆多任务（多供应商 / 多平台）。
type PurchaseTask struct {
	ID              string          `gorm:"primaryKey;type:varchar(32)" json:"id"`
	OrderID         string          `gorm:"column:order_id;type:varchar(32)" json:"order_id"`
	SupplierOfferID *string         `gorm:"column:supplier_offer_id;type:varchar(32)" json:"supplier_offer_id"`
	Channel         string          `gorm:"type:varchar(16)" json:"channel"`
	ExecutorType    string          `gorm:"column:executor_type;type:varchar(16)" json:"executor_type"`
	Status          string          `gorm:"type:varchar(16)" json:"status"`
	Payload         json.RawMessage `gorm:"type:json" json:"payload"`
	Assignee        *string         `gorm:"type:varchar(64)" json:"assignee"`
	Deadline        *time.Time      `json:"deadline"`
	IdempotencyKey  *string         `gorm:"column:idempotency_key;type:varchar(128)" json:"idempotency_key"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DelFlag         bool            `json:"del_flag"`
}

// TableName 显式表名。
func (PurchaseTask) TableName() string { return "purchase_tasks" }

// PurchaseOrder 采购单（表 purchase_orders）：实际下单结果与价格快照。
// 国内快递号只在内部用，**不回传 Ozon**（总纲 §7.4）。
type PurchaseOrder struct {
	ID                 string          `gorm:"primaryKey;type:varchar(32)" json:"id"`
	TaskID             string          `gorm:"column:task_id;type:varchar(32)" json:"task_id"`
	PlatformOrderID    string          `gorm:"column:platform_order_id;type:varchar(64)" json:"platform_order_id"`
	Amount             decimal.Decimal `gorm:"type:decimal(18,4)" json:"amount"`
	Currency           string          `gorm:"type:char(3)" json:"currency"`
	PaidAt             *time.Time      `gorm:"column:paid_at" json:"paid_at"`
	DomesticCarrier    *string         `gorm:"column:domestic_carrier;type:varchar(64)" json:"domestic_carrier"`
	DomesticTrackingNo *string         `gorm:"column:domestic_tracking_no;type:varchar(64)" json:"domestic_tracking_no"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	DelFlag            bool            `json:"del_flag"`
}

// TableName 显式表名。
func (PurchaseOrder) TableName() string { return "purchase_orders" }

// TaskPayload 采购任务的下单参数快照（总纲 §5.1：payload = 下单参数快照）。
type TaskPayload struct {
	StoreID       string            `json:"store_id"`
	PostingNumber string            `json:"posting_number"`
	RelayPointID  string            `json:"relay_point_id"`
	RelayName     string            `json:"relay_name"`
	RelayAddress  string            `json:"relay_address"`
	RelayContact  string            `json:"relay_contact"`
	ShipDeadline  *time.Time        `json:"ship_deadline"`
	Currency      string            `json:"currency"`
	Items         []TaskPayloadItem `json:"items"`
	Note          string            `json:"note,omitempty"` // 流转备注（如自动转人工的原因）

	// OrderAttemptedAt 下单尝试时刻（调 1688 下单接口**之前**落库）。
	// 用途：重试时若「核对不到那张单」（消息丢了 / 平台没回传 outOrderId），
	// 说明我们确实下过单——此时不再自动下单，转异常池让人去 1688 后台核。
	// （重复采购要花钱；停下来只花人的几分钟。PR #19 轻审 #1，owner 拍板。）
	OrderAttemptedAt *time.Time `json:"order_attempted_at,omitempty"`
}

// TaskPayloadItem 快照里的一行（价格是下单当时的映射报价，供事后对账 / 变价告警）。
type TaskPayloadItem struct {
	OzonOfferID string          `json:"ozon_offer_id"`
	ItemID      string          `json:"item_id"`
	SkuID       string          `json:"sku_id"`
	URL         string          `json:"url"`
	Qty         int             `json:"qty"`
	UnitPrice   decimal.Decimal `json:"unit_price"`
	Currency    string          `json:"currency"`
}

// DecodePayload 解析快照；坏载荷返回零值 + error。
func (t *PurchaseTask) DecodePayload() (TaskPayload, error) {
	var p TaskPayload
	if len(t.Payload) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return p, err
	}
	return p, nil
}

// DomesticTracking 国内段单号（交接对照表用；只在内部流转，不回传 Ozon）。
type DomesticTracking struct {
	OrderID    string
	Carrier    string
	TrackingNo string
}
