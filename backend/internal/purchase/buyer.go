// 1688 买家侧能力的「使用方接口」（ADR-20261007-go-package-deps：接口放使用方）。
// 实现方是 internal/alibaba（S1-C，方法清单见其子 spec §3）；装配层把它适配成这组签名。
// 1688 凭据是企业级（store_id 空，总纲 §6），工厂只收凭据、不收店铺。
package purchase

import (
	"context"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/shopspring/decimal"
)

// BuyerClient S1-D 需要的 1688 五件套（对应子 spec C §3 的方法清单）。
type BuyerClient interface {
	// PreviewOrder 下单预览：运费与「能不能下单」（新商家 / 地址问题在这里暴露）。
	PreviewOrder(ctx context.Context, req PreviewRequest) (*PreviewResult, error)
	// CreateOrder 下单（alibaba.trade.fastCreateOrder）：收货地址 = 中转点，买家留言带 posting_number。
	CreateOrder(ctx context.Context, req CreateOrderRequest) (*BuyerOrder, error)
	// GetOrder 订单详情（记已付款时核对用）。
	GetOrder(ctx context.Context, platformOrderID string) (*BuyerOrder, error)
	// ListBuyerOrders 按时间窗查买家订单（下单防重核对用）。
	ListBuyerOrders(ctx context.Context, req ListBuyerOrdersRequest) ([]BuyerOrder, error)
	// GetLogistics 买家视角物流：国内段单号与轨迹。
	GetLogistics(ctx context.Context, platformOrderID string) (*Logistics, error)
}

// BuyerCredentials 1688 的企业级凭据（都不挂店，总纲 §6）：
// 应用密钥（alibaba_app）+ 买家 token（alibaba_token，C 包自己负责续期）。
type BuyerCredentials struct {
	App   *store.Decrypted
	Token *store.Decrypted
}

// BuyerClientFactory 装配层注入：用企业级凭据造客户端。S1-C 未合并前为 nil，
// 自动执行器会明确报错（重试用尽进异常池），人工流程不受影响。
type BuyerClientFactory func(creds BuyerCredentials) (BuyerClient, error)

// 下单预览的失败原因（装配层把 S1-C 的错误归一到这里）。
const (
	// PreviewReasonNewSeller 新商家：买家自用版只能对下过单的老商家下单（总纲 §5.1、§3.2）
	// → 首单自动转人工。
	PreviewReasonNewSeller = "new_seller"
	// PreviewReasonAddressInvalid 地址校验失败 → 异常池。
	PreviewReasonAddressInvalid = "address_invalid"
	// PreviewReasonOther 其它原因（限流、库存等）→ 走重试。
	PreviewReasonOther = "other"
)

// Address 收货地址（= 中转点地址；不含买家信息，总纲 §7.3）。
type Address struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

// PreviewRequest 下单预览请求。
type PreviewRequest struct {
	ItemID    string          `json:"item_id"`
	SkuID     string          `json:"sku_id"`
	Qty       int             `json:"qty"`
	UnitPrice decimal.Decimal `json:"unit_price"`
	Address   Address         `json:"address"`
}

// PreviewResult 预览结果。
type PreviewResult struct {
	CanOrder bool            `json:"can_order"`
	Reason   string          `json:"reason,omitempty"` // new_seller / address_invalid / other
	Message  string          `json:"message,omitempty"`
	Freight  decimal.Decimal `json:"freight"`
}

// CreateOrderRequest 下单请求。
type CreateOrderRequest struct {
	ItemID    string          `json:"item_id"`
	SkuID     string          `json:"sku_id"`
	Qty       int             `json:"qty"`
	UnitPrice decimal.Decimal `json:"unit_price"`
	Address   Address         `json:"address"`
	// Remark 买家留言：必须含 posting_number（中转点认包的凭据，总纲 §5.7）。
	Remark string `json:"remark"`
	// OutOrderID 外部订单号（1688 幂等字段，官方建议恒定传入）：下单防重核对
	// 以它为准——比在留言里做字符串匹配可靠得多（S1-B/C 已合并后补，见适配器 PR）。
	OutOrderID string `json:"out_order_id"`
}

// BuyerOrder 买家订单（1688 侧）。
type BuyerOrder struct {
	PlatformOrderID string `json:"platform_order_id"`
	// OutOrderID 下单时带的外部订单号（核对「这单是不是本任务下的」的权威凭据）。
	OutOrderID string          `json:"out_order_id"`
	Amount     decimal.Decimal `json:"amount"`
	Currency   string          `json:"currency"`
	Status     string          `json:"status"`
	PaidAt     *time.Time      `json:"paid_at,omitempty"`
	Carrier    string          `json:"carrier,omitempty"`
	TrackingNo string          `json:"tracking_no,omitempty"`
	Remark     string          `json:"remark,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ListBuyerOrdersRequest 买家订单查询（时间窗）。
type ListBuyerOrdersRequest struct {
	Since time.Time `json:"since"`
	To    time.Time `json:"to"`
}

// Logistics 买家视角物流（国内段）。
type Logistics struct {
	Carrier     string    `json:"carrier"`
	TrackingNo  string    `json:"tracking_no"`
	LatestEvent string    `json:"latest_event,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}
