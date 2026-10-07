package ozon

import (
	"context"
	"errors"
	"fmt"
)

// DefaultListLimit 拉单默认每页条数（官方未给默认值；社区实践上限 1000）。
const DefaultListLimit = 1000

// 排序方向（官方 schema 枚举 ASC / DESC）。
const (
	SortAsc  = "ASC"
	SortDesc = "DESC"
)

// ErrInvalidParams 调用参数不合法（本地校验，不会发出请求）。
var ErrInvalidParams = errors.New("ozon: 参数不合法")

// PostingWith 附加数据开关（官方 with 参数；全部 false = 不发送 with）。
type PostingWith struct {
	AnalyticsData bool `json:"analytics_data,omitempty"`
	Barcodes      bool `json:"barcodes,omitempty"`
	FinancialData bool `json:"financial_data,omitempty"`
	LegalInfo     bool `json:"legal_info,omitempty"`
}

// ListPostingsParams 拉单参数（/v4/posting/fbs/list）。
type ListPostingsParams struct {
	// Since / To 时间窗（必填；官方 filter.since / filter.to）。
	Since, To Time
	// Statuses 按寄件状态过滤（可选；官方枚举见 Posting.Status 注释）。
	Statuses []string
	// Limit 每页条数；0 = DefaultListLimit。
	Limit int64
	// Cursor 翻页游标（上一页响应的 Cursor，首页留空）。
	Cursor string
	// SortDir 排序方向；空 = SortAsc。
	SortDir string
	// With 附加数据开关。
	With PostingWith
}

// PostingsPage 一页寄件（/v4/posting/fbs/list 响应）。
type PostingsPage struct {
	Postings []Posting `json:"postings"`
	HasNext  bool      `json:"has_next"`
	Cursor   string    `json:"cursor"`
}

type listPostingsFilter struct {
	Since    Time     `json:"since,omitzero"`
	To       Time     `json:"to,omitzero"`
	Statuses []string `json:"statuses,omitempty"`
}

type listPostingsRequest struct {
	Filter  listPostingsFilter `json:"filter"`
	Limit   int64              `json:"limit,omitempty"`
	Cursor  string             `json:"cursor,omitempty"`
	SortDir string             `json:"sort_dir,omitempty"`
	With    *PostingWith       `json:"with,omitempty"`
}

// ListPostings 按时间窗、状态拉单（总纲 §7.1：FBS/rFBS 用 /v4/posting/fbs/list，
// v3 已于 2026-08-31 关停）。翻页：首调用 Cursor 留空，HasNext=true 时把
// 响应的 Cursor 填进下一次请求。
func (c *Client) ListPostings(ctx context.Context, p ListPostingsParams) (*PostingsPage, error) {
	if p.Since.IsZero() || p.To.IsZero() {
		return nil, fmt.Errorf("%w: ListPostings 需要时间窗 Since/To", ErrInvalidParams)
	}
	req := listPostingsRequest{
		Filter:  listPostingsFilter{Since: p.Since, To: p.To, Statuses: p.Statuses},
		Limit:   p.Limit,
		Cursor:  p.Cursor,
		SortDir: p.SortDir,
	}
	if req.Limit == 0 {
		req.Limit = DefaultListLimit
	}
	if p.With != (PostingWith{}) {
		req.With = &p.With
	}
	var page PostingsPage
	if err := c.do(ctx, EndpointPostingList, pathPostingList, req, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ListUnfulfilledParams 拉未完成单参数（/v4/posting/fbs/unfulfilled/list）。
// 官方 filter 支持 cutoff（须组装完成时间）与 delivering_date（交运时间）两个时间窗，
// 均可留空 = 不限。
type ListUnfulfilledParams struct {
	// CutoffFrom / CutoffTo 按「卖家须组装完成的时间」过滤（可选）。
	CutoffFrom, CutoffTo Time
	// DeliveringDateFrom / DeliveringDateTo 按「寄件交运时间」过滤（可选）。
	DeliveringDateFrom, DeliveringDateTo Time
	// Statuses 按状态过滤（可选）。
	Statuses []string
	// Limit 每页条数；0 = DefaultListLimit。
	Limit int64
	// Cursor 翻页游标。
	Cursor string
	// SortDir 排序方向；空 = SortAsc。
	SortDir string
	// With 附加数据开关。
	With PostingWith
}

// UnfulfilledPage 一页未完成寄件（/v4/posting/fbs/unfulfilled/list 响应）。
type UnfulfilledPage struct {
	Postings []Posting `json:"postings"`
	HasNext  bool      `json:"has_next"`
	Cursor   string    `json:"cursor"`
	Count    int64     `json:"count"`
}

type listUnfulfilledFilter struct {
	CutoffFrom         Time     `json:"cutoff_from,omitzero"`
	CutoffTo           Time     `json:"cutoff_to,omitzero"`
	DeliveringDateFrom Time     `json:"delivering_date_from,omitzero"`
	DeliveringDateTo   Time     `json:"delivering_date_to,omitzero"`
	Statuses           []string `json:"statuses,omitempty"`
}

type listUnfulfilledRequest struct {
	Filter  listUnfulfilledFilter `json:"filter"`
	Limit   int64                 `json:"limit,omitempty"`
	Cursor  string                `json:"cursor,omitempty"`
	SortDir string                `json:"sort_dir,omitempty"`
	With    *PostingWith          `json:"with,omitempty"`
}

// ListUnfulfilled 拉未完成单。
func (c *Client) ListUnfulfilled(ctx context.Context, p ListUnfulfilledParams) (*UnfulfilledPage, error) {
	req := listUnfulfilledRequest{
		Filter: listUnfulfilledFilter{
			CutoffFrom:         p.CutoffFrom,
			CutoffTo:           p.CutoffTo,
			DeliveringDateFrom: p.DeliveringDateFrom,
			DeliveringDateTo:   p.DeliveringDateTo,
			Statuses:           p.Statuses,
		},
		Limit:   p.Limit,
		Cursor:  p.Cursor,
		SortDir: p.SortDir,
	}
	if req.Limit == 0 {
		req.Limit = DefaultListLimit
	}
	if p.With != (PostingWith{}) {
		req.With = &p.With
	}
	var page UnfulfilledPage
	if err := c.do(ctx, EndpointPostingUnfulfilled, pathPostingUnfulfilled, req, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

type getPostingRequest struct {
	PostingNumber string `json:"posting_number"`
}

// GetPosting 单详情（/v3/posting/fbs/get）。返回 detail 里的 status、substatus、
// tpl_integration_type、shipment_date、parent_posting_number 等字段（S1-D 据此
// 复核备货结果与判定单号回传）。
func (c *Client) GetPosting(ctx context.Context, postingNumber string) (*PostingDetail, error) {
	if postingNumber == "" {
		return nil, fmt.Errorf("%w: GetPosting 需要 postingNumber", ErrInvalidParams)
	}
	var resp struct {
		Result *PostingDetail `json:"result"`
	}
	if err := c.do(ctx, EndpointPostingGet, pathPostingGet,
		getPostingRequest{PostingNumber: postingNumber}, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("GetPosting %s: 响应缺少 result 字段", postingNumber)
	}
	return resp.Result, nil
}

// ShipProduct 一个要组装进包裹的商品。
type ShipProduct struct {
	// ProductID 商品标识：官方文档口径为 SKU（ozon 侧商品 ID）。
	ProductID int64 `json:"product_id"`
	// Quantity 数量。
	Quantity int32 `json:"quantity"`
}

// ShipPackage 一个包裹（订单拆多包裹时 packages 传多个）。
type ShipPackage struct {
	Products []ShipProduct `json:"products"`
}

// ShipResult 备货响应（官方：result 为拆分后生成的寄件号列表）。
//
// ⚠️ HTTP 200 不保证组装成功：调用方须随后用 GetPosting 复核
// substatus（出现 ship_failed 要重试组装）——官方文档明确要求。
type ShipResult struct {
	// PostingNumbers 组装后生成的寄件号（拆单时多于一个）。
	PostingNumbers []string `json:"result"`
	// AdditionalData 附加信息（当前请求未开启 with.additional_data，恒为空，占位备查）。
	AdditionalData []ShipAdditional `json:"additional_data"`
}

// ShipAdditional 备货附加信息（with.additional_data=true 时返回）。
type ShipAdditional struct {
	PostingNumber string `json:"posting_number"`
}

type shipRequest struct {
	Packages      []ShipPackage `json:"packages"`
	PostingNumber string        `json:"posting_number"`
}

// ShipPosting 备货（组装寄件，/v4/posting/fbs/ship）：把订单拆成寄件并转
// awaiting_deliver。packages 给「包裹 → 商品」列表；单包裹传一个元素。
//
// ⚠️ 返回后必须复核 substatus（见 ShipResult 注释）——本方法不做复核，
// 由调用方（S1-D）执行。
func (c *Client) ShipPosting(ctx context.Context, postingNumber string, packages []ShipPackage) (*ShipResult, error) {
	if postingNumber == "" {
		return nil, fmt.Errorf("%w: ShipPosting 需要 postingNumber", ErrInvalidParams)
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("%w: ShipPosting 需要至少一个包裹", ErrInvalidParams)
	}
	for i, pkg := range packages {
		if len(pkg.Products) == 0 {
			return nil, fmt.Errorf("%w: ShipPosting 第 %d 个包裹没有商品", ErrInvalidParams, i+1)
		}
	}
	var resp ShipResult
	req := shipRequest{Packages: packages, PostingNumber: postingNumber}
	if err := c.do(ctx, EndpointPostingShip, pathPostingShip, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
