package ozon

import (
	"context"
	"fmt"
)

// maxLabelPostings 旧面单接口单请求寄件号上限（官方明文：не больше 20）。
const maxLabelPostings = 20

type packageLabelRequest struct {
	PostingNumbers []string `json:"posting_number"`
}

// GetPackageLabel 取面单 PDF（旧接口 /v2/posting/fbs/package-label，同步返回二进制）。
//
// ⚠️ 官方公告该接口 2026-11-02 关停，替代为两步流程
// CreatePackageLabel → GetPackageLabelTask（见 labeltask.go）。保留本方法仅为兼容；
// 新调用一律走两步流程。
//
// 限制（官方）：一次最多 20 个寄件号；任一条出错则全部不生成；
// 建议组装后 45–60 秒再请求。
func (c *Client) GetPackageLabel(ctx context.Context, postingNumbers []string) ([]byte, error) {
	if len(postingNumbers) == 0 {
		return nil, fmt.Errorf("%w: GetPackageLabel 需要至少一个寄件号", ErrInvalidParams)
	}
	if len(postingNumbers) > maxLabelPostings {
		return nil, fmt.Errorf("%w: GetPackageLabel 单请求最多 %d 个寄件号（官方限制）", ErrInvalidParams, maxLabelPostings)
	}
	body, err := c.doRaw(ctx, EndpointPostingLabel, pathPostingLabel,
		packageLabelRequest{PostingNumbers: postingNumbers})
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("GetPackageLabel %v: 空响应", postingNumbers)
	}
	return body, nil
}
