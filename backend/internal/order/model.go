// Package order 是「订单域」（总纲 §5.2）：轮询拉单、Ozon 状态映射、内部状态机、
// 异常池（exceptions 表）、订单接口。
//
// 依赖方向（ADR-20261007-go-package-deps）：order 依赖 store；purchase、shipment 依赖 order。
// 本包不许 import purchase / shipment——反方向（新订单要生成采购任务）走 asynq 任务。
package order

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// 内部状态机（总纲 §5.2）：new → purchasing → purchased → inbound → at_relay
// → handed_over → in_transit → delivered → completed；旁支 cancelled / returned。
const (
	StatusNew        = "new"
	StatusPurchasing = "purchasing"
	StatusPurchased  = "purchased"
	StatusInbound    = "inbound"
	StatusAtRelay    = "at_relay"
	StatusHandedOver = "handed_over"
	StatusInTransit  = "in_transit"
	StatusDelivered  = "delivered"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
	StatusReturned   = "returned"
)

// statusRank 主线状态的先后（只前进不后退，总纲 §5.9）；旁支不参与排序。
var statusRank = map[string]int{
	StatusNew:        0,
	StatusPurchasing: 1,
	StatusPurchased:  2,
	StatusInbound:    3,
	StatusAtRelay:    4,
	StatusHandedOver: 5,
	StatusInTransit:  6,
	StatusDelivered:  7,
	StatusCompleted:  8,
}

// IsTerminal 旁支终态（取消 / 退货）：不再被任何状态推进。
func IsTerminal(status string) bool {
	return status == StatusCancelled || status == StatusReturned
}

// Rank 主线状态序号；旁支与未知返回 -1。
func Rank(status string) int {
	if r, ok := statusRank[status]; ok {
		return r
	}
	return -1
}

// StatusesMainline S1 主线全部状态（接口筛选用）。
func StatusesMainline() []string {
	return []string{
		StatusNew, StatusPurchasing, StatusPurchased, StatusInbound,
		StatusAtRelay, StatusHandedOver, StatusInTransit, StatusDelivered, StatusCompleted,
	}
}

// ErrOrderNotFound 订单不存在 / 已软删。
var ErrOrderNotFound = errors.New("订单不存在")

// Order 订单（表 orders，posting 粒度；唯一键 store_id + posting_number 幂等）。
type Order struct {
	ID                  string          `gorm:"primaryKey;type:varchar(32)" json:"id"`
	StoreID             string          `gorm:"column:store_id;type:varchar(32)" json:"store_id"`
	PostingNumber       string          `gorm:"column:posting_number;type:varchar(64)" json:"posting_number"`
	OrderNumber         string          `gorm:"column:order_number;type:varchar(64)" json:"order_number"`
	ParentPostingNumber *string         `gorm:"column:parent_posting_number;type:varchar(64)" json:"parent_posting_number"`
	Status              string          `gorm:"type:varchar(32)" json:"status"`
	OzonStatus          string          `gorm:"column:ozon_status;type:varchar(48)" json:"ozon_status"`
	OzonSubstatus       *string         `gorm:"column:ozon_substatus;type:varchar(48)" json:"ozon_substatus"`
	TplIntegrationType  *string         `gorm:"column:tpl_integration_type;type:varchar(32)" json:"tpl_integration_type"`
	ShipDeadline        *time.Time      `gorm:"column:ship_deadline" json:"ship_deadline"`
	RelayPointID        *string         `gorm:"column:relay_point_id;type:varchar(32)" json:"relay_point_id"`
	BuyerEnc            []byte          `gorm:"column:buyer_enc;type:blob" json:"-"`
	TotalAmount         decimal.Decimal `gorm:"column:total_amount;type:decimal(18,4)" json:"total_amount"`
	Currency            string          `gorm:"type:char(3)" json:"currency"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	DelFlag             bool            `json:"del_flag"`
}

// TableName 显式表名。
func (Order) TableName() string { return "orders" }

// OrderItem 订单行（表 order_items）。
type OrderItem struct {
	ID          string          `gorm:"primaryKey;type:varchar(32)" json:"id"`
	OrderID     string          `gorm:"column:order_id;type:varchar(32)" json:"order_id"`
	OzonOfferID string          `gorm:"column:ozon_offer_id;type:varchar(128)" json:"ozon_offer_id"`
	Qty         int             `json:"qty"`
	Price       decimal.Decimal `gorm:"type:decimal(18,4)" json:"price"`
	Currency    string          `gorm:"type:char(3)" json:"currency"`
	OfferLinkID *string         `gorm:"column:offer_link_id;type:varchar(32)" json:"offer_link_id"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	DelFlag     bool            `json:"del_flag"`
}

// TableName 显式表名。
func (OrderItem) TableName() string { return "order_items" }

// CalcTotal 订单总额 = Σ 行价格 × 数量（商品行货币一致时才有意义；不一致就按第一行币种记）。
func CalcTotal(items []OrderItem) (decimal.Decimal, string) {
	total := decimal.Zero
	currency := "CNY"
	for i, it := range items {
		total = total.Add(it.Price.Mul(decimal.NewFromInt(int64(it.Qty))))
		if i == 0 && it.Currency != "" {
			currency = it.Currency
		}
	}
	return total, currency
}
