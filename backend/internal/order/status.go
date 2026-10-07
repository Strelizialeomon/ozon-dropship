// Ozon 状态 → 内部动作的映射表（总纲 §5.2，【官方】枚举）。
//
// 三条口径：
//   - Ozon 状态原样另存（orders.ozon_status / ozon_substatus），映射只决定「内部动作」；
//   - new → … → at_relay 由本系统的采购与中转流程推进，Ozon 状态不反向改写；
//   - 表外的任何状态进异常池并告警（防官方新增状态被静默吞掉）。
package order

// Ozon posting 状态（FBS / rFBS；总纲 §5.2 映射表左列逐条落成常量）。
const (
	OzonAwaitingRegistration = "awaiting_registration"
	OzonAcceptanceInProgress = "acceptance_in_progress"
	OzonAwaitingApprove      = "awaiting_approve"
	OzonAwaitingVerification = "awaiting_verification"
	OzonAwaitingPackaging    = "awaiting_packaging"
	OzonAwaitingDeliver      = "awaiting_deliver"
	OzonDelivering           = "delivering"
	OzonDriverPickup         = "driver_pickup"
	OzonSentBySeller         = "sent_by_seller"
	OzonDelivered            = "delivered"
	OzonCancelled            = "cancelled"
	OzonNotAccepted          = "not_accepted"
	OzonArbitration          = "arbitration"
	OzonClientArbitration    = "client_arbitration"
)

// platformHeldStatuses 「平台未放行」：只建单为 new，不生成采购任务（总纲 §5.2）。
var platformHeldStatuses = map[string]bool{
	OzonAwaitingRegistration: true,
	OzonAcceptanceInProgress: true,
	OzonAwaitingApprove:      true,
	OzonAwaitingVerification: true,
}

// StatusRule 一条映射规则。
type StatusRule struct {
	// AllowedToPurchase 允许生成采购任务（仅 awaiting_packaging）。
	AllowedToPurchase bool
	// ForwardStatus 要推进到的内部状态；空 = 不动（内部状态由本系统流程推进）。
	ForwardStatus string
	// ExceptionCode 进异常池的码；空 = 不进。
	ExceptionCode string
	// Known 是否在映射表内（false = 表外状态，进异常池并告警）。
	Known bool
}

// RuleFor 查 Ozon 状态的映射规则。表外状态：Known=false + 异常码 unknown_status。
//
// 注意与总纲 §5.2 表逐行对应；改这里必须同步改总纲映射表（spec-flow 修订同步规矩）。
func RuleFor(ozonStatus string) StatusRule {
	switch ozonStatus {
	case OzonAwaitingRegistration, OzonAcceptanceInProgress, OzonAwaitingApprove, OzonAwaitingVerification:
		// 建单为 new、标「平台未放行」，不生成采购任务。
		return StatusRule{Known: true}
	case OzonAwaitingPackaging:
		// 允许生成采购任务；内部状态由本系统流程推进（Ozon 状态不反向改写）。
		return StatusRule{Known: true, AllowedToPurchase: true}
	case OzonAwaitingDeliver:
		return StatusRule{Known: true, ForwardStatus: StatusHandedOver}
	case OzonDelivering, OzonDriverPickup, OzonSentBySeller:
		return StatusRule{Known: true, ForwardStatus: StatusInTransit}
	case OzonDelivered:
		return StatusRule{Known: true, ForwardStatus: StatusDelivered}
	case OzonCancelled, OzonNotAccepted:
		return StatusRule{Known: true, ForwardStatus: StatusCancelled}
	case OzonArbitration, OzonClientArbitration:
		return StatusRule{Known: true, ExceptionCode: CodeArbitration}
	default:
		return StatusRule{Known: false, ExceptionCode: CodeUnknownStatus}
	}
}

// IsPlatformHeld 该 Ozon 状态下订单处于「平台未放行」——超时扫描要跳过它们
// （没放行就不该催采购，责任不在我们）。
func IsPlatformHeld(ozonStatus string) bool { return platformHeldStatuses[ozonStatus] }

// ---- 传单号规则（总纲 §7.4，五个 tpl_integration_type 取值）----

// tpl_integration_type 取值（官方定义）。
const (
	TplOzon          = "ozon"           // Ozon 自有配送：单号由 Ozon 生成，只读不传
	TplAggregator    = "aggregator"     // 外部承运商、由 Ozon 登记：也只读不传
	Tpl3PLTracking   = "3pl_tracking"   // 外部承运商、由卖家登记：我们传
	TplNonIntegrated = "non_integrated" // 卖家自行配送：我们传
	// TplHybryd 俄罗斯邮政混合方案：posting 体系（§7.4 的判定依据）官方原文拼写是 hybryd，
	// 总纲 §7.4 v1.4 已按官方同步。
	TplHybryd = "hybryd"
	// TplHybrid delivery-method 体系的同名字段写作 hybrid（总纲 §7.4 注）。两版都认：
	// 无论数据来自哪套体系都进异常池，不许因为拼写之争漏判。
	TplHybrid = "hybrid"
)

// TrackingAction 传单号动作。
type TrackingAction int

const (
	TrackingNone  TrackingAction = iota // 不传（ozon / aggregator：单号由 Ozon 生成，我们只读）
	TrackingSet                         // 传（3pl_tracking / non_integrated）
	TrackingBlock                       // 进异常池（hybrid：本项目暂不涉及）
)

// TrackingActionFor 按 tpl_integration_type 决定传单号动作。
// 值不在五个取值内 → 同样进异常池（与表外状态同一口径：静默吞掉最危险）。
func TrackingActionFor(tpl string) (TrackingAction, string) {
	switch tpl {
	case TplOzon, TplAggregator:
		return TrackingNone, ""
	case Tpl3PLTracking, TplNonIntegrated:
		return TrackingSet, ""
	case TplHybryd, TplHybrid:
		return TrackingBlock, CodeHybridTpl
	default:
		return TrackingBlock, CodeUnknownTpl
	}
}
