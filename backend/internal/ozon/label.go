package ozon

import (
	"context"
	"fmt"
)

type packageLabelRequest struct {
	PostingNumbers []string `json:"posting_number"`
}

// GetPackageLabel 取面单 PDF（/v2/posting/fbs/package-label，同步返回二进制）。
// 一次可传多个寄件号，返回的是合并 PDF。文件本体由调用方落盘/转存（S1-D）。
func (c *Client) GetPackageLabel(ctx context.Context, postingNumbers []string) ([]byte, error) {
	if len(postingNumbers) == 0 {
		return nil, fmt.Errorf("%w: GetPackageLabel 需要至少一个寄件号", ErrInvalidParams)
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
