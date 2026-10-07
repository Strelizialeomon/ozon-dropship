package ozon

import (
	"context"
	"fmt"
)

// TrackingNumber 一条「寄件号 → 承运商单号」。
type TrackingNumber struct {
	PostingNumber  string `json:"posting_number"`
	TrackingNumber string `json:"tracking_number"`
}

// SetTrackingNumberResult 单条回传结果（官方 result[] 的元素）。
type SetTrackingNumberResult struct {
	PostingNumber string `json:"posting_number"`
	// OK 该条是否成功。
	OK bool `json:"result"`
	// Error 失败原因（成功时为空串）。
	Error string `json:"error"`
}

type setTrackingNumberRequest struct {
	TrackingNumbers []TrackingNumber `json:"tracking_numbers"`
}

// SetTrackingNumber 回传承运商单号（/v2/fbs/posting/tracking-number/set）。
//
// ⚠️ 仅当 tpl_integration_type ∈ {3pl_tracking, non_integrated} 时调用
// （总纲 §7.4）；ozon / aggregator 的单号由 Ozon 生成，传了反而错。
// 是否该调由调用方按 GetPosting 的字段判定。
//
// 接口本身按条返回成败（HTTP 200 但个别条 error 非空），逐条结果交调用方处置。
func (c *Client) SetTrackingNumber(ctx context.Context, items []TrackingNumber) ([]SetTrackingNumberResult, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: SetTrackingNumber 需要至少一条", ErrInvalidParams)
	}
	for i, it := range items {
		if it.PostingNumber == "" || it.TrackingNumber == "" {
			return nil, fmt.Errorf("%w: SetTrackingNumber 第 %d 条缺 posting_number 或 tracking_number", ErrInvalidParams, i+1)
		}
	}
	var resp struct {
		Result []SetTrackingNumberResult `json:"result"`
	}
	req := setTrackingNumberRequest{TrackingNumbers: items}
	if err := c.do(ctx, EndpointTrackingNumberSet, pathTrackingNumberSet, req, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}
