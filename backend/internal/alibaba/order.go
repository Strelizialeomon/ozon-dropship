package alibaba

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// Address 收货地址（下单/预览的 addressParam）。
// 字段与官方文档 alibaba.trade.fast.address 对齐；S1 里填的是中转点地址（总纲 §5.7）。
type Address struct {
	FullName     string `json:"fullName"`
	Mobile       string `json:"mobile"`
	Phone        string `json:"phone,omitempty"`    // 官方标必填但实际可为空串，如无座机给空
	PostCode     string `json:"postCode,omitempty"` // 邮编
	ProvinceText string `json:"provinceText"`
	CityText     string `json:"cityText"`
	AreaText     string `json:"areaText"`
	TownText     string `json:"townText,omitempty"` // 镇/街道，可为空
	Address      string `json:"address"`            // 街道详细地址
	DistrictCode string `json:"districtCode,omitempty"`
}

// Cargo 一条采购商品（cargoParamList 的元素）。
type Cargo struct {
	// OfferID 1688 商品 id（offerId）。
	OfferID uint64 `json:"offerId"`
	// SpecID 规格 id（specId，从商品详情接口原样取，不能自己拼）。
	SpecID string `json:"specId"`
	// Quantity 采购数量（官方类型是 Double，整数按整数量传）。
	Quantity float64 `json:"quantity"`
}

// PreviewOrderRequest 下单预览请求。
type PreviewOrderRequest struct {
	// Address 收货地址（S1 = 中转点）。
	Address Address
	// CargoList 商品列表（同一供应商合单；跨供应商要拆单调用）。
	CargoList []Cargo
	// Flow 下单通道；空 = general（大市场批发单）。
	Flow string
	// OutOrderID 外部订单号（幂等，可选）。
	OutOrderID string
}

// PreviewOrderResult 预览结果。
type PreviewOrderResult struct {
	// OrderPreviews 预览明细；自动拆单时会返回多条。
	OrderPreviews []OrderPreview `json:"orderPreviews"`
	// PostFeeByDescOfferList 运费按描述的商品列表。
	PostFeeByDescOfferList []int64 `json:"postFeeByDescOfferList"`
	// ConsignOfferList 代销商品列表。
	ConsignOfferList []int64 `json:"consignOfferList"`
}

// OrderPreview 一条预览明细（orderPreviewResuslt 的元素）。
type OrderPreview struct {
	// Status 该组合能否下单。
	Status bool `json:"status"`
	// FlowFlag 下单通道标记。
	FlowFlag string `json:"flowFlag"`
	// ResultCode / Message 不能下单时的原因。
	ResultCode string `json:"resultCode"`
	Message    string `json:"message"`
	// 金额单位：分（官方字段说明）。
	SumPayment           int64 `json:"sumPayment"`           // 应付总额
	SumCarriage          int64 `json:"sumCarriage"`          // 运费
	SumPaymentNoCarriage int64 `json:"sumPaymentNoCarriage"` // 不含运费的货值
	DiscountFee          int64 `json:"discountFee"`
	AdditionalFee        int64 `json:"additionalFee"`
	// TradeModeNameList 可用交易方式（tradeType 从这里面选）。
	TradeModeNameList []string `json:"tradeModeNameList"`
	// CargoList 每条商品的算价结果。
	CargoList []PreviewCargo `json:"cargoList"`
}

// PreviewCargo 预览里一条商品的算价（单位：分）。
type PreviewCargo struct {
	OfferID        uint64     `json:"offerId"`
	SpecID         string     `json:"specId"`
	SKUID          FlexString `json:"skuId"`
	Amount         int64      `json:"amount"`
	FinalUnitPrice int64      `json:"finalUnitPrice"`
}

// CreateOrderRequest 下单请求（alibaba.trade.fastCreateOrder，买家自用版通道一）。
type CreateOrderRequest struct {
	// Address 收货地址（S1 = 中转点地址）。
	Address Address
	// CargoList 商品列表（同一供应商合单）。
	CargoList []Cargo
	// Message 买家留言；S1 约定写 posting_number 供中转点认包（总纲 §5.7）。
	Message string
	// Flow 下单通道；空 = general。
	Flow string
	// OutOrderID 外部订单号（幂等，可选）。下单失败但可能已执行时，调用方靠它/查单核对后
	// 再决定是否重发；建议 S1-D 恒定传入。
	OutOrderID string
}

// CreateOrderResult 下单结果。
type CreateOrderResult struct {
	// OrderID 下单成功后的 1688 订单 id。
	OrderID FlexString `json:"orderId"`
	// TotalSuccessAmount 订单总金额，单位：分。
	TotalSuccessAmount int64 `json:"totalSuccessAmount"`
	// PostFee 原始运费，单位：分（下单后卖家可能调整，不等于最终支付运费）。
	PostFee int64 `json:"postFee"`
	// FailedOfferList 部分商品下单失败的信息（官方类型 offer[]；整体 success 仍为 true 时出现）。
	FailedOfferList []FailedOffer `json:"failedOfferList"`
}

// FailedOffer 下单失败的一条商品（failedOfferList 的元素）。
type FailedOffer struct {
	OfferID      FlexString `json:"offerId"`
	SpecID       string     `json:"specId"`
	ErrorCode    string     `json:"errorCode"`
	ErrorMessage string     `json:"errorMessage"`
}

// Order 订单详情/列表里的一条订单（官方 TradeInfo；只建 S1 用得到 + 常见的域）。
type Order struct {
	BaseInfo        OrderBaseInfo      `json:"baseInfo"`
	ProductItems    []OrderProductItem `json:"productItems"`
	TradeTerms      []TradeTerm        `json:"tradeTerms"`
	NativeLogistics *NativeLogistics   `json:"nativeLogistics"` // 国内物流
}

// PaidAmount 实付金额（元）= 各期付款额（tradeTerms[].phasAmount）之和。
// S1 单期付款时等于 totalAmount；分期交易必须累加 tradeTerms，别只看单条。
// （totalAmount 是「应付款总金额」，两者口径不同；S1-D 记账取这个。）
func (o *Order) PaidAmount() decimal.Decimal {
	sum := decimal.Zero
	for _, t := range o.TradeTerms {
		sum = sum.Add(t.PhasAmount)
	}
	return sum
}

// OrderBaseInfo 订单基础信息（金额单位：除注明外为元）。
type OrderBaseInfo struct {
	// ID / IDOfStr 交易 id（字符串语义：官方文档也说用 idOfStr 处理，防 JS/PHP 精度问题；
	// 本包统一用 FlexString，不受 int64 上限约束）。
	ID      FlexString `json:"id"`
	IDOfStr FlexString `json:"idOfStr"`
	// Status 交易状态：waitbuyerpay / waitsellersend / waitbuyerreceive / confirm_goods / cancel ...
	Status string `json:"status"`
	// BusinessType 业务类型：cn(普通) / ws(大额批发) / yp(拿样) / yf(分销) ...
	BusinessType string `json:"businessType"`
	// 时间轴。
	CreateTime   FlexTime `json:"createTime"`
	PayTime      FlexTime `json:"payTime"` // 首次付款时间
	ModifyTime   FlexTime `json:"modifyTime"`
	CompleteTime FlexTime `json:"completeTime"`
	// TotalAmount 应付款总金额 = ΣitemAmount + 运费，单位：元。
	TotalAmount decimal.Decimal `json:"totalAmount"`
	// SumProductPayment 商品金额合计，单位：元。
	SumProductPayment decimal.Decimal `json:"sumProductPayment"`
	// ShippingFee 运费，单位：元。
	ShippingFee decimal.Decimal `json:"shippingFee"`
	// Discount 折扣信息，单位：分。
	Discount int64 `json:"discount"`
	// Refund 退款金额，单位：元；RefundPayment 退款金额（分）。
	Refund        decimal.Decimal `json:"refund"`
	RefundPayment int64           `json:"refundPayment"`
	// RefundStatus 售中退款状态（waitselleragree/...），空 = 无退款。
	RefundStatus string `json:"refundStatus"`
	// CloseReason 关闭原因（buyerCancel / sellerGoodsLack / other）。
	CloseReason string `json:"closeReason"`
	// TradeType 交易类型码（1 担保交易、2 预存款、... 见官方枚举）。
	TradeType string `json:"tradeType"`
	// OutOrderID 下单时带的外部订单号。
	OutOrderID string `json:"outOrderId"`
	// AlipayTradeID 外部支付交易 id。
	AlipayTradeID string `json:"alipayTradeId"`

	// 买卖双方身份。
	SellerID      string `json:"sellerID"`
	SellerLoginID string `json:"sellerLoginId"`
	SellerUserID  int64  `json:"sellerUserId"`
	BuyerID       string `json:"buyerID"`
	BuyerLoginID  string `json:"buyerLoginId"`
	BuyerUserID   int64  `json:"buyerUserId"`

	// 联系人/收件人信息。
	BuyerContact  *TradeContact `json:"buyerContact"`
	SellerContact *TradeContact `json:"sellerContact"`
	ReceiverInfo  *ReceiverInfo `json:"receiverInfo"`
}

// TradeContact 买卖联系人。
type TradeContact struct {
	Phone        string `json:"phone"`
	Mobile       string `json:"mobile"`
	Name         string `json:"name"`
	CompanyName  string `json:"companyName"`
	IMInPlatform string `json:"imInPlatform"` // 旺旺 id
}

// ReceiverInfo 收件人信息（买家视角）。
type ReceiverInfo struct {
	ToFullName     string `json:"toFullName"`
	ToDivisionCode string `json:"toDivisionCode"`
	ToPost         string `json:"toPost"`
	ToTownCode     string `json:"toTownCode"`
	ToArea         string `json:"toArea"`
}

// OrderProductItem 订单里的一条商品明细（金额单位：除注明外为元）。
type OrderProductItem struct {
	// ProductID 商品 id（offerId）；SpecID / SKUID 规格；SubItemID 子订单号。
	ProductID       FlexString      `json:"productID"`
	SpecID          string          `json:"specId"`
	SKUID           FlexString      `json:"skuID"`
	SubItemID       FlexString      `json:"subItemID"`
	SubItemIDStr    FlexString      `json:"subItemIDString"`
	Name            string          `json:"name"`
	Price           decimal.Decimal `json:"price"`           // 原始单价（元）
	ItemAmount      decimal.Decimal `json:"itemAmount"`      // 实付金额（元）
	Quantity        decimal.Decimal `json:"quantity"`        // 以 Unit 为单位的数量
	Unit            string          `json:"unit"`            // 个/件/箱/吨...
	Status          string          `json:"status"`          // 子订单状态
	StatusStr       string          `json:"statusStr"`       // 子订单状态描述
	LogisticsStatus int             `json:"logisticsStatus"` // 1 未发货 2 已发货 3 已收货 4 已退货 5 部分发货 8 未创建物流单
	Refund          decimal.Decimal `json:"refund"`          // 退款金额（元）
	RefundStatus    string          `json:"refundStatus"`
	CloseReason     string          `json:"closeReason"`
	// CargoNumber / ProductCargoNumber 货号。
	CargoNumber        string `json:"cargoNumber"`
	ProductCargoNumber string `json:"productCargoNumber"`
	// ProductImgURL 商品图；ProductSnapshotURL 下单时的商品快照。
	ProductImgURL      []string `json:"productImgUrl"`
	ProductSnapshotURL string   `json:"productSnapshotUrl"`
	// SkuInfos 规格描述（颜色/尺码...）。
	SkuInfos []SkuInfo `json:"skuInfos"`
	// EntryDiscount 明细涨价/降价金额（分）。
	EntryDiscount int64 `json:"entryDiscount"`
}

// SkuInfo 规格名值对。
type SkuInfo struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// TradeTerm 交易条款（付款）。金额单位:元。
type TradeTerm struct {
	// PayStatus 支付状态（1688：8 等数字码；国际站另有枚举）。
	PayStatus string `json:"payStatus"`
	// PayTime 完成阶段支付时间。
	PayTime FlexTime `json:"payTime"`
	// PayWay 支付方式（1 支付宝、2 网商银行信任付、3 诚e赊...）。
	PayWay     string          `json:"payWay"`
	PayWayDesc string          `json:"payWayDesc"`
	PhasAmount decimal.Decimal `json:"phasAmount"` // 付款额（元）
	Phase      FlexString      `json:"phase"`      // 阶段单 id
}

// NativeLogistics 订单里的国内物流域（nativeLogistics）。
type NativeLogistics struct {
	Address       string                `json:"address"`
	Area          string                `json:"area"`
	AreaCode      string                `json:"areaCode"`
	City          string                `json:"city"`
	Province      string                `json:"province"`
	ContactPerson string                `json:"contactPerson"`
	Mobile        string                `json:"mobile"`
	Telephone     string                `json:"telephone"`
	Zip           string                `json:"zip"`
	TownCode      string                `json:"townCode"`
	Town          string                `json:"town"`
	Items         []NativeLogisticsItem `json:"logisticsItems"`
}

// NativeLogisticsItem 国内物流运单明细。
type NativeLogisticsItem struct {
	ID              FlexString `json:"id"`
	LogisticsCode   string     `json:"logisticsCode"`
	LogisticsBillNo string     `json:"logisticsBillNo"` // 运单号（如卖家已发货）
	Status          string     `json:"status"`
	Type            string     `json:"type"`
	FromPhone       string     `json:"fromPhone"`
	FromMobile      string     `json:"fromMobile"`
	SubItemIDs      string     `json:"subItemIds"`
	GmtCreate       FlexTime   `json:"gmtCreate"`
	GmtModified     FlexTime   `json:"gmtModified"`
	DeliveredTime   FlexTime   `json:"deliveredTime"`
}

// ListBuyerOrdersRequest 买家订单列表查询（下单防重核对用：按时间窗拉）。
type ListBuyerOrdersRequest struct {
	// CreateStartTime / CreateEndTime 下单时间窗（必给的核对维度；零值 = 不传）。
	CreateStartTime time.Time
	CreateEndTime   time.Time
	// OrderStatus 订单状态过滤；空 = 全部。
	OrderStatus string
	// Page / PageSize 分页；0 = 默认（page 1、pageSize 50）。
	Page     int
	PageSize int
	// OrderIDs 指定订单 id 查询（可选）。
	OrderIDs []uint64
	// IsHis 是否查历史订单表；默认 false（当前表）。
	IsHis bool
}

// BuyerOrderList 订单列表结果。
type BuyerOrderList struct {
	// Orders 订单列表。
	Orders []Order `json:"orders"`
	// TotalRecord 总记录数。
	TotalRecord int64 `json:"totalRecord"`
}

// defaultPageSize 列表默认每页条数（官方未定默认值，这里定 50）。
const defaultPageSize = 50

// defaultOrderIncludeFields GetOrder 默认要求的扩展域：国内物流（运单号在里面）。
const defaultOrderIncludeFields = "NativeLogistics"

// PreviewOrder 下单预览：校验能否下单、算运费/优惠（alibaba.createOrder.preview）。
func (c *Client) PreviewOrder(ctx context.Context, req PreviewOrderRequest) (*PreviewOrderResult, error) {
	params := map[string]any{
		"addressParam":   req.Address,
		"cargoParamList": req.CargoList,
		"flow":           defaultFlow(req.Flow),
	}
	if req.OutOrderID != "" {
		params["outOrderId"] = req.OutOrderID
	}

	var out PreviewOrderResult
	err := c.do(ctx, apiPreview, params, true, false, func(body []byte) error {
		var raw struct {
			statusFields
			OrderPreviews          []OrderPreview `json:"orderPreviewResuslt"`
			PostFeeByDescOfferList []int64        `json:"postFeeByDescOfferList"`
			ConsignOfferList       []int64        `json:"consignOfferList"`
		}
		if err := unmarshalBody(apiPreview, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiPreview.name); ae != nil {
			return c.fail(ae)
		}
		if len(raw.OrderPreviews) == 0 {
			return badResponse(apiPreview, "响应缺 orderPreviewResuslt", body)
		}
		out = PreviewOrderResult{
			OrderPreviews:          raw.OrderPreviews,
			PostFeeByDescOfferList: raw.PostFeeByDescOfferList,
			ConsignOfferList:       raw.ConsignOfferList,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateOrder 下单（alibaba.trade.fastCreateOrder，买家自用版）。
// 注意：
//   - 下单只创建订单，不含付款；付款由人工在 1688 完成（S1 双轨，总纲 §9·S1）；
//   - 本方法**不做盲目重试**：5xx、超时、响应解不出来都可能是「订单已建、回包丢了」，
//     这类失败直接上抛；调用方（S1-D 的防重流程）核对后再决定补记还是重发。
//     只有确定被拒、没执行的失败（429/超限）才会在内部重试。
//   - 部分商品可能失败（result.failedOfferList 非空、整体 success 仍为 true）——
//     调用方要检查这个列表。
func (c *Client) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	params := map[string]any{
		"addressParam":   req.Address,
		"cargoParamList": req.CargoList,
		"flow":           defaultFlow(req.Flow),
	}
	if req.Message != "" {
		params["message"] = req.Message
	}
	if req.OutOrderID != "" {
		params["outOrderId"] = req.OutOrderID
	}

	var out CreateOrderResult
	err := c.do(ctx, apiFastCreateOrder, params, true, true, func(body []byte) error {
		var raw struct {
			statusFields
			Result *CreateOrderResult `json:"result"`
		}
		if err := unmarshalBody(apiFastCreateOrder, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiFastCreateOrder.name); ae != nil {
			return c.fail(ae)
		}
		if raw.Result == nil || raw.Result.OrderID == "" {
			return badResponse(apiFastCreateOrder, "响应缺 result.orderId", body)
		}
		out = *raw.Result
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrder 订单详情（alibaba.trade.get.buyerView）：状态、金额、商品明细、国内物流。
func (c *Client) GetOrder(ctx context.Context, orderID uint64) (*Order, error) {
	params := map[string]any{
		"webSite":       "1688",
		"orderId":       strconv.FormatUint(orderID, 10),
		"includeFields": defaultOrderIncludeFields,
	}

	var out Order
	err := c.do(ctx, apiGetBuyerView, params, true, false, func(body []byte) error {
		var raw struct {
			statusFields
			Result *Order `json:"result"`
		}
		if err := unmarshalBody(apiGetBuyerView, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiGetBuyerView.name); ae != nil {
			return c.fail(ae)
		}
		if raw.Result == nil {
			return badResponse(apiGetBuyerView, "响应缺 result", body)
		}
		out = *raw.Result
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListBuyerOrders 按条件查买家订单（alibaba.trade.getBuyerOrderList）。
func (c *Client) ListBuyerOrders(ctx context.Context, req ListBuyerOrdersRequest) (*BuyerOrderList, error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	params := map[string]any{
		"webSite":  "1688",
		"page":     strconv.Itoa(page),
		"pageSize": strconv.Itoa(pageSize),
	}
	if !req.CreateStartTime.IsZero() {
		params["createStartTime"] = req.CreateStartTime.Format(gatewayTimeLayout)
	}
	if !req.CreateEndTime.IsZero() {
		params["createEndTime"] = req.CreateEndTime.Format(gatewayTimeLayout)
	}
	if req.OrderStatus != "" {
		params["orderStatus"] = req.OrderStatus
	}
	if len(req.OrderIDs) > 0 {
		params["orderIds"] = req.OrderIDs
	}
	if req.IsHis {
		params["isHis"] = "true"
	}

	var out BuyerOrderList
	err := c.do(ctx, apiGetBuyerOrderList, params, true, false, func(body []byte) error {
		var raw struct {
			statusFields
			// Result 用 RawMessage 先接住：「result 键缺了/null」和「查无订单（result: []）」
			// 必须区分——下游是下单防重核对，把响应形状漂移读成「没下过单」会导致重复下单。
			Result      json.RawMessage `json:"result"`
			TotalRecord int64           `json:"totalRecord"`
		}
		if err := unmarshalBody(apiGetBuyerOrderList, body, &raw); err != nil {
			return err
		}
		if ae := raw.err(apiGetBuyerOrderList.name); ae != nil {
			return c.fail(ae)
		}
		if len(raw.Result) == 0 || string(raw.Result) == "null" {
			return badResponse(apiGetBuyerOrderList, "响应缺 result（形状漂移，不能当作「查无订单」）", body)
		}
		var orders []Order
		if err := json.Unmarshal(raw.Result, &orders); err != nil {
			return fmt.Errorf("alibaba %s: 解 result 失败: %w", apiGetBuyerOrderList.name, err)
		}
		out = BuyerOrderList{Orders: orders, TotalRecord: raw.TotalRecord}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// defaultFlow 下单通道默认 general（大市场批发单）；S1 暂不做分销/跨境通道。
func defaultFlow(flow string) string {
	if flow == "" {
		return "general"
	}
	return flow
}

// badResponse 构造一个不重试的响应结构错误（响应缺关键字段这类）。
func badResponse(a api, msg string, body []byte) error {
	return fail(&APIError{API: a.name, Code: "BAD_RESPONSE", Message: msg, Body: truncateBody(body)})
}
