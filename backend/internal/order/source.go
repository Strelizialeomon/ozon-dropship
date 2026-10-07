// Ozon 拉单能力的「使用方接口」（ADR-20261007-go-package-deps：接口放使用方）。
// 实现方是 internal/ozon（S1-B，方法清单见其子 spec §3）；装配层把它的返回类型
// 适配成这里的 Posting / PostingItem 传进来。S1-B 未合并前工厂为 nil，拉单任务会明确报错。
package order

import (
	"context"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/shopspring/decimal"
)

// PostingSource S1-D 需要的 Ozon 拉单三件套。
type PostingSource interface {
	// ListPostings 按时间窗拉单（/v4/posting/fbs/list）。
	ListPostings(ctx context.Context, req ListPostingsRequest) ([]Posting, error)
	// ListUnfulfilled 拉未完成单（/v4/posting/fbs/unfulfilled/list）——
	// 时间窗拉不到「早就建好、刚变状态」的老单，未完成单列表兜住这部分。
	ListUnfulfilled(ctx context.Context, req ListPostingsRequest) ([]Posting, error)
	// GetPosting 单详情（/v3/posting/fbs/get）：status / substatus /
	// tpl_integration_type / shipment_date / 商品行。
	GetPosting(ctx context.Context, postingNumber string) (*Posting, error)
}

// PostingSourceFactory 装配层注入：给定店铺与解密后的凭据造一个客户端。
type PostingSourceFactory func(shop store.Shop, cred *store.Decrypted) (PostingSource, error)

// ListPostingsRequest 拉单请求（时间窗 + 可选状态过滤）。
type ListPostingsRequest struct {
	Since    time.Time
	To       time.Time
	Statuses []string // 空 = 不限
}

// PostingItem 商品行（只取下单要用的字段；买家信息一律不拉，总纲 §10 第 7 条）。
type PostingItem struct {
	OzonOfferID string
	Qty         int
	Price       decimal.Decimal
	Currency    string
}

// Posting 一条 posting（拆单后以 posting 为单位）。
type Posting struct {
	PostingNumber       string
	OrderNumber         string
	ParentPostingNumber string
	Status              string
	Substatus           string
	TplIntegrationType  string
	ShipDeadline        *time.Time // shipment_date：发货截止
	Items               []PostingItem
	Currency            string
}

// TotalAmount 订单总额 = Σ 行价格 × 数量。
func (p Posting) TotalAmount() decimal.Decimal {
	total := decimal.Zero
	for _, it := range p.Items {
		total = total.Add(it.Price.Mul(decimal.NewFromInt(int64(it.Qty))))
	}
	return total
}
