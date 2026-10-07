package order

import "testing"

// 验收：§5.2 映射表**每一行**都有单测（含「平台未放行」不建采购任务、表外状态进异常池）。
func TestRuleForMappingTableEveryRow(t *testing.T) {
	cases := []struct {
		name    string
		ozon    string
		allowed bool // 允许生成采购任务
		forward string
		exc     string
		known   bool
	}{
		{"awaiting_registration 未放行", OzonAwaitingRegistration, false, "", "", true},
		{"acceptance_in_progress 未放行", OzonAcceptanceInProgress, false, "", "", true},
		{"awaiting_approve 未放行", OzonAwaitingApprove, false, "", "", true},
		{"awaiting_verification 未放行", OzonAwaitingVerification, false, "", "", true},
		{"awaiting_packaging 允许采购且不反向改写状态", OzonAwaitingPackaging, true, "", "", true},
		{"awaiting_deliver → handed_over", OzonAwaitingDeliver, false, StatusHandedOver, "", true},
		{"delivering → in_transit", OzonDelivering, false, StatusInTransit, "", true},
		{"driver_pickup → in_transit", OzonDriverPickup, false, StatusInTransit, "", true},
		{"sent_by_seller → in_transit", OzonSentBySeller, false, StatusInTransit, "", true},
		{"delivered → delivered", OzonDelivered, false, StatusDelivered, "", true},
		{"cancelled → cancelled", OzonCancelled, false, StatusCancelled, "", true},
		{"not_accepted → cancelled", OzonNotAccepted, false, StatusCancelled, "", true},
		{"arbitration 进异常池", OzonArbitration, false, "", CodeArbitration, true},
		{"client_arbitration 进异常池", OzonClientArbitration, false, "", CodeArbitration, true},
		{"表外状态进异常池", "brand_new_status_from_2027", false, "", CodeUnknownStatus, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := RuleFor(tc.ozon)
			if r.AllowedToPurchase != tc.allowed {
				t.Errorf("AllowedToPurchase = %v，期望 %v", r.AllowedToPurchase, tc.allowed)
			}
			if r.ForwardStatus != tc.forward {
				t.Errorf("ForwardStatus = %q，期望 %q", r.ForwardStatus, tc.forward)
			}
			if r.ExceptionCode != tc.exc {
				t.Errorf("ExceptionCode = %q，期望 %q", r.ExceptionCode, tc.exc)
			}
			if r.Known != tc.known {
				t.Errorf("Known = %v，期望 %v", r.Known, tc.known)
			}
		})
	}
}

// 「平台未放行」的四个状态不允许生成采购任务（另一条独立的验收口径）。
func TestPlatformHeldNeverAllowsPurchase(t *testing.T) {
	held := []string{OzonAwaitingRegistration, OzonAcceptanceInProgress, OzonAwaitingApprove, OzonAwaitingVerification}
	for _, st := range held {
		if !IsPlatformHeld(st) {
			t.Errorf("%s 应算「平台未放行」", st)
		}
		if RuleFor(st).AllowedToPurchase {
			t.Errorf("%s 不应允许生成采购任务", st)
		}
	}
	// awaiting_packaging 是表里唯一「允许采购」的状态，不算未放行。
	if IsPlatformHeld(OzonAwaitingPackaging) {
		t.Error("awaiting_packaging 不应算「平台未放行」")
	}
}

// 验收：tpl_integration_type 五个取值各有单测（ozon / aggregator 不传；
// 3pl_tracking / non_integrated 传；hybrid 进异常池）。
func TestTrackingActionForFiveTplValues(t *testing.T) {
	cases := []struct {
		tpl    string
		action TrackingAction
		code   string
	}{
		{TplOzon, TrackingNone, ""},
		{TplAggregator, TrackingNone, ""},
		{Tpl3PLTracking, TrackingSet, ""},
		{TplNonIntegrated, TrackingSet, ""},
		{TplHybrid, TrackingBlock, CodeHybridTpl},
		// 官方原文拼写是 hybryd（S1-B 实测）：拼写两版都要认，不许漏判成 unknown_tpl。
		{TplHybryd, TrackingBlock, CodeHybridTpl},
		{"something_else", TrackingBlock, CodeUnknownTpl},
	}
	for _, tc := range cases {
		t.Run(tc.tpl, func(t *testing.T) {
			action, code := TrackingActionFor(tc.tpl)
			if action != tc.action {
				t.Errorf("action = %v，期望 %v", action, tc.action)
			}
			if code != tc.code {
				t.Errorf("code = %q，期望 %q", code, tc.code)
			}
		})
	}
}

// 状态推进规则：只前进、不后退；取消 / 退货是旁支终态。
func TestShouldAdvance(t *testing.T) {
	cases := []struct {
		current, target string
		want            bool
	}{
		{StatusNew, StatusPurchasing, true},
		{StatusPurchasing, StatusNew, false}, // 不后退
		{StatusAtRelay, StatusHandedOver, true},
		{StatusHandedOver, StatusAtRelay, false},
		{StatusNew, StatusNew, false},             // 原地不动
		{StatusHandedOver, StatusCancelled, true}, // 取消随时生效
		{StatusCancelled, StatusInTransit, false}, // 终态不再动
		{StatusReturned, StatusCompleted, false},
		// 已送达 / 已完成之后不再被取消覆盖（拒收 / 退回属 S3 退货域的流程）。
		{StatusDelivered, StatusCancelled, false},
		{StatusCompleted, StatusCancelled, false},
	}
	for _, tc := range cases {
		if got := shouldAdvance(tc.current, tc.target); got != tc.want {
			t.Errorf("shouldAdvance(%q, %q) = %v，期望 %v", tc.current, tc.target, got, tc.want)
		}
	}
}

// 发货截止临近扫描的状态集合：只包括还没交运的主线状态。
func TestStatusesBelowHandedOver(t *testing.T) {
	got := StatusesBelow(StatusHandedOver)
	want := map[string]bool{StatusNew: true, StatusPurchasing: true, StatusPurchased: true, StatusInbound: true, StatusAtRelay: true}
	if len(got) != len(want) {
		t.Fatalf("状态集合大小 = %d，期望 %d（%v）", len(got), len(want), got)
	}
	for _, st := range got {
		if !want[st] {
			t.Errorf("不应包含状态 %q", st)
		}
	}
}
