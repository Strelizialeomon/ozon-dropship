package ozon

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Time 容忍空串与 null 的时间：Ozon 时间字段缺值时给 ""，直接解析进
// time.Time 会报错。零值表示「无」。
type Time struct{ time.Time }

// UnmarshalJSON 兼容 "2026-04-01T19:47:39.878Z" / "" / null。
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("解析 Ozon 时间 %q 失败: %w", s, err)
	}
	t.Time = parsed
	return nil
}

// MarshalJSON 请求参数用：零值发 null（配合 omitzero 的字段会直接省略）。
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

// IsZero 配合请求结构的 omitzero。
func (t Time) IsZero() bool { return t.Time.IsZero() }

// TplIntegrationType 取值（官方现行文档原文；S1-D 据此判定是否回传单号，总纲 §7.4）。
//
// ⚠️ 注意官方原文把混合方案拼作 `hybryd`；总纲里写作 `hybrid`，实解析按官方。
const (
	TplIntegrationOzon          = "ozon"           // Ozon 自有配送：单号由 Ozon 生成，只读不传
	TplIntegrationAggregator    = "aggregator"     // 外部承运商、Ozon 登记订单：同上
	TplIntegration3PLTracking   = "3pl_tracking"   // 外部承运商、卖家登记订单：由我们传单号
	TplIntegrationNonIntegrated = "non_integrated" // 卖家自行配送：传单号 + 报轨迹三段
	TplIntegrationHybryd        = "hybryd"         // 俄罗斯邮政混合方案：S1 暂不涉及，遇到进异常池
)

// Addressee 收件人（v4 postings[].addressee / v3 result.addressee 的并集）。
type Addressee struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Pin   string `json:"pin"`
}

// Customer 买家（v4 postings[].customer / v3 result.customer 的并集）。
type Customer struct {
	Address       *CustomerAddress `json:"address"`
	CustomerEmail string           `json:"customer_email"` // 仅 v4
	CustomerID    int64            `json:"customer_id"`
	Name          string           `json:"name"`
	Phone         string           `json:"phone"`
}

// CustomerAddress 收货地址。
type CustomerAddress struct {
	AddressTail    string  `json:"address_tail"`
	City           string  `json:"city"`
	Comment        string  `json:"comment"`
	Country        string  `json:"country"`
	District       string  `json:"district"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	ProviderPvzCode string `json:"provider_pvz_code"`
	PvzCode        int64   `json:"pvz_code"`
	Region         string  `json:"region"`
	ZipCode        string  `json:"zip_code"`
}

// DeliveryMethod 配送方式（v4 / v3 结构一致）。
type DeliveryMethod struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	TplProvider   string `json:"tpl_provider"`
	TplProviderID int64  `json:"tpl_provider_id"`
	Warehouse     string `json:"warehouse"`
	WarehouseID   int64  `json:"warehouse_id"`
}

// AnalyticsData 附加分析数据（需 with.analytics_data=true；v4 / v3 并集）。
type AnalyticsData struct {
	City                    string `json:"city"`
	Region                  string `json:"region"`
	DeliveryType            string `json:"delivery_type"`
	IsLegal                 bool   `json:"is_legal"`
	IsPremium               bool   `json:"is_premium"`
	PaymentTypeGroupName    string `json:"payment_type_group_name"`
	TplProvider             string `json:"tpl_provider"`
	TplProviderID           int64  `json:"tpl_provider_id"`
	Warehouse               string `json:"warehouse"`
	WarehouseID             int64  `json:"warehouse_id"`
	DeliveryDateBegin       Time   `json:"delivery_date_begin"`
	DeliveryDateEnd         Time   `json:"delivery_date_end"`
	ClientDeliveryDateBegin Time   `json:"client_delivery_date_begin"`
	ClientDeliveryDateEnd   Time   `json:"client_delivery_date_end"`
}

// Money 金额（官方 money.postingMoney：amount 为字符串）。
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Barcode 面单上下条码（需 with.barcodes=true）。
type Barcode struct {
	LowerBarcode string `json:"lower_barcode"`
	UpperBarcode string `json:"upper_barcode"`
}

// Cancellation 取消信息（仅 v4 列表项）。
type Cancellation struct {
	AffectCancellationRating bool   `json:"affect_cancellation_rating"`
	CancelReason             string `json:"cancel_reason"`
	CancelReasonID           int64  `json:"cancel_reason_id"`
	CancellationInitiator    string `json:"cancellation_initiator"`
	CancellationType         string `json:"cancellation_type"`
	CancelledAfterShip       bool   `json:"cancelled_after_ship"`
}

// PRROption 装卸服务选项（v3 单详情口径是对象；v4 列表项同名同段是字符串，
// 见 Posting.PRROption）。
type PRROption struct {
	Code         string `json:"code"`
	Price        string `json:"price"`
	CurrencyCode string `json:"currency_code"`
	Floor        string `json:"floor"`
}

// Posting 寄件（对应 /v4/posting/fbs/list 与 /v4/posting/fbs/unfulfilled/list 的
// postings[]，官方 schema posting.v4.PostingFbsListResponse.Postings）。
//
// v3 单详情是另一套结构（价格字段类型不同），见 PostingDetail。
type Posting struct {
	PostingNumber            string          `json:"posting_number"`
	OrderID                  int64           `json:"order_id"`
	OrderNumber              string          `json:"order_number"`
	ParentPostingNumber      string          `json:"parent_posting_number"`
	Status                   string          `json:"status"`
	Substatus                string          `json:"substatus"`
	TplIntegrationType       string          `json:"tpl_integration_type"`
	IntegrationTypeFlow      string          `json:"integration_type_flow"`
	ShipmentDate             Time            `json:"shipment_date"`
	ShipmentDateWithoutDelay Time            `json:"shipment_date_without_delay"`
	InProcessAt              Time            `json:"in_process_at"`
	DeliveringDate           Time            `json:"delivering_date"`
	TrackingNumber           string          `json:"tracking_number"`
	IsMultibox               bool            `json:"is_multibox"`
	MultiBoxQty              int32           `json:"multi_box_qty"`
	PRROption                string          `json:"prr_option"`
	ScanIt                   string          `json:"scanit"`
	DeliverySchema           string          `json:"delivery_schema"`
	AvailableActions         []string        `json:"available_actions"`
	Addressee                *Addressee      `json:"addressee"`
	Customer                 *Customer       `json:"customer"`
	DeliveryMethod           *DeliveryMethod `json:"delivery_method"`
	AnalyticsData            *AnalyticsData  `json:"analytics_data"`
	FinancialData            *ListFinancialData `json:"financial_data"`
	Products                 []ListProduct   `json:"products"`
	Barcodes                 *Barcode        `json:"barcodes"`
	Cancellation             *Cancellation   `json:"cancellation"`
}

// ListProduct 列表项里的商品（官方 posting.v4...Postings.Products）。
type ListProduct struct {
	Name         string   `json:"name"`
	OfferID      string   `json:"offer_id"`
	SKU          int64    `json:"sku"`
	Quantity     int32    `json:"quantity"`
	Price        *Money   `json:"price"`
	ProductColor string   `json:"product_color"`
	Weight       float64  `json:"weight"`
	IMEI         []string `json:"imei"`
}

// ListFinancialData 列表项的财务数据（官方 posting.v4...Postings.FinancialData）。
type ListFinancialData struct {
	ClusterFrom string                `json:"cluster_from"`
	ClusterTo   string                `json:"cluster_to"`
	Products    []ListFinancialProduct `json:"products"`
}

// ListFinancialProduct 列表项财务里的单商品。
type ListFinancialProduct struct {
	Actions               []string    `json:"actions"`
	Commission            *Commission `json:"commission"`
	CustomerPrice         *Money      `json:"customer_price"`
	OldPrice              float64     `json:"old_price"`
	Payout                float64     `json:"payout"`
	Price                 float64     `json:"price"`
	ProductID             int64       `json:"product_id"`
	Quantity              int64       `json:"quantity"`
	TotalDiscountPercent  float64     `json:"total_discount_percent"`
	TotalDiscountValue    float64     `json:"total_discount_value"`
}

// Commission 佣金（列表项口径：amount + currency + percent）。
type Commission struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Percent  int64   `json:"percent"`
}

// PostingDetail 单详情（对应 /v3/posting/fbs/get 的 result，
// 官方 schema v3FbsPostingDetail）。比列表项多 previous_substatus 等字段，
// 商品与财务字段的类型和列表项不同（见下）。
type PostingDetail struct {
	PostingNumber            string          `json:"posting_number"`
	OrderID                  int64           `json:"order_id"`
	OrderNumber              string          `json:"order_number"`
	ParentPostingNumber      string          `json:"parent_posting_number"`
	Status                   string          `json:"status"`
	Substatus                string          `json:"substatus"`
	PreviousSubstatus        string          `json:"previous_substatus"`
	TplIntegrationType       string          `json:"tpl_integration_type"`
	IntegrationTypeFlow      string          `json:"integration_type_flow"`
	ProviderStatus           string          `json:"provider_status"`
	ShipmentDate             Time            `json:"shipment_date"`
	ShipmentDateWithoutDelay Time            `json:"shipment_date_without_delay"`
	InProcessAt              Time            `json:"in_process_at"`
	DeliveringDate           Time            `json:"delivering_date"`
	FactDeliveryDate         Time            `json:"fact_delivery_date"`
	TrackingNumber           string          `json:"tracking_number"`
	IsMultibox               bool            `json:"is_multibox"`
	MultiBoxQty              int32           `json:"multi_box_qty"`
	PRROption                *PRROption      `json:"prr_option"`
	ScanIt                   string          `json:"scanit"`
	AvailableActions         []string        `json:"available_actions"`
	Addressee                *Addressee      `json:"addressee"`
	Customer                 *Customer       `json:"customer"`
	DeliveryMethod           *DeliveryMethod `json:"delivery_method"`
	AnalyticsData            *AnalyticsData  `json:"analytics_data"`
	FinancialData            *DetailFinancialData `json:"financial_data"`
	Products                 []DetailProduct `json:"products"`
	Barcodes                 *Barcode        `json:"barcodes"`
	Cancellation             *Cancellation   `json:"cancellation"`
	RelatedPostings          []string        `json:"related_postings"`
}

// DetailProduct 单详情里的商品（官方 v3PostingProductDetail；price 是字符串、
// 币种单列 currency_code，与列表项的结构不同）。
type DetailProduct struct {
	Name          string      `json:"name"`
	OfferID       string      `json:"offer_id"`
	SKU           int64       `json:"sku"`
	Quantity      int32       `json:"quantity"`
	Price         string      `json:"price"`
	CurrencyCode  string      `json:"currency_code"`
	MandatoryMark []string    `json:"mandatory_mark"`
	Dimensions    *Dimensions `json:"dimensions"`
	IsBlrTraceable bool       `json:"is_blr_traceable"`
	HasIMEI       bool        `json:"has_imei"`
}

// Dimensions 商品包装尺寸（v3 详情）。
type Dimensions struct {
	Height string `json:"height"`
	Length string `json:"length"`
	Weight string `json:"weight"`
	Width  string `json:"width"`
}

// DetailFinancialData 单详情的财务数据（官方 v3PostingFinancialData；佣金是按商品
// 平铺的 commission_amount / commission_percent，与列表项的对象结构不同）。
type DetailFinancialData struct {
	ClusterFrom string                  `json:"cluster_from"`
	ClusterTo   string                  `json:"cluster_to"`
	Products    []DetailFinancialProduct `json:"products"`
}

// DetailFinancialProduct 单详情财务里的单商品。
type DetailFinancialProduct struct {
	Actions               []string `json:"actions"`
	CurrencyCode          string   `json:"currency_code"`
	CustomerCurrencyCode  string   `json:"customer_currency_code"`
	CommissionAmount      float64  `json:"commission_amount"`
	CommissionPercent     int64    `json:"commission_percent"`
	CommissionsCurrencyCode string `json:"commissions_currency_code"`
	CustomerPrice         float64  `json:"customer_price"`
	OldPrice              float64  `json:"old_price"`
	Payout                float64  `json:"payout"`
	Price                 float64  `json:"price"`
	ProductID             int64    `json:"product_id"`
	Quantity              int64    `json:"quantity"`
	TotalDiscountPercent  float64  `json:"total_discount_percent"`
	TotalDiscountValue    float64  `json:"total_discount_value"`
}
