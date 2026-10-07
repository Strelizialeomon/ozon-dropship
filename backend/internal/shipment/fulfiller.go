// Ozon 履约动作的「使用方接口」（ADR-20261007-go-package-deps：接口放使用方）。
// 实现方是 internal/ozon（S1-B，方法清单见其子 spec §3）；装配层适配进来。
package shipment

import (
	"context"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
)

// PostingFulfiller S1-D 用到的 Ozon 履约动作。
type PostingFulfiller interface {
	// ShipPosting 备货（/v4/posting/fbs/ship，多包裹用 /ship/package）。
	// ⚠️ 返回 200 不代表成功，调用方必须再查 substatus（总纲 §5.2）。
	ShipPosting(ctx context.Context, postingNumber string) error
	// GetPosting 查 posting 当前状态（复核 substatus 用）。
	GetPosting(ctx context.Context, postingNumber string) (*PostingState, error)
	// GetPackageLabel 面单 PDF（/v2/posting/fbs/package-label，同步返回）。
	GetPackageLabel(ctx context.Context, postingNumber string) ([]byte, error)
	// SetTrackingNumber 传单号（/v2/fbs/posting/tracking-number/set）——
	// 只在 tpl_integration_type 为 3pl_tracking / non_integrated 时调（总纲 §7.4）。
	SetTrackingNumber(ctx context.Context, postingNumber, trackingNo, carrier string) error
}

// PostingState posting 状态摘要（复核用）。
type PostingState struct {
	Status    string
	Substatus string
}

// FulfillerFactory 装配层注入：给定店铺与凭据造客户端。S1-B 未合并前为 nil，
// 备货 / 面单接口会明确报错。
type FulfillerFactory func(shop store.Shop, cred *store.Decrypted) (PostingFulfiller, error)

// SubstatusShipFailed 备货「返回成功但实际没成」的 substatus（官方口径，总纲 §5.2）。
const SubstatusShipFailed = "ship_failed"
