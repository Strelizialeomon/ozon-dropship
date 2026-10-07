package ozon

import (
	"context"
)

// DeliveryMethodStatus 配送方式状态（官方枚举）。
const (
	DeliveryMethodNew      = "NEW"
	DeliveryMethodEdited   = "EDITED"
	DeliveryMethodActive   = "ACTIVE"
	DeliveryMethodDisabled = "DISABLED"
	DeliveryMethodWaiting  = "WAITING"
	DeliveryMethodBroken   = "BROKEN"
)

// DropOffPoint 3PL 收件点（官方 tpl_dropoff_point）。
type DropOffPoint struct {
	Address     string       `json:"address"`
	Coordinates *Coordinates `json:"address_coordinates"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
}

// Coordinates 地理坐标。
type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// DeliveryMethodInfo 一个配送方式（/v2/delivery-method/list 的 delivery_methods[]）。
type DeliveryMethodInfo struct {
	ID                int64         `json:"id"`
	Name              string        `json:"name"`
	Status            string        `json:"status"`
	ProviderID        int64         `json:"provider_id"`
	TemplateID        int64         `json:"template_id"`
	TplIntegrationType string       `json:"tpl_integration_type"`
	WarehouseID       int64         `json:"warehouse_id"`
	Cutoff            string        `json:"cutoff"`
	SlaCutIn          int64         `json:"sla_cut_in"`
	IsExpress         bool          `json:"is_express"`
	CreatedAt         Time          `json:"created_at"`
	UpdatedAt         Time          `json:"updated_at"`
	DropOffPoint      *DropOffPoint `json:"tpl_dropoff_point"`
}

// ListDeliveryMethodsParams 物流方式列表参数（/v2/delivery-method/list）。
type ListDeliveryMethodsParams struct {
	// Statuses 按状态过滤（可选；取值见 DeliveryMethod* 常量）。
	Statuses []string
	// Limit 每页条数；0 = DefaultListLimit。
	Limit int64
	// Cursor 翻页游标。
	Cursor string
	// SortDir 排序方向；空 = SortAsc。
	SortDir string
}

// DeliveryMethodsPage 一页物流方式（/v2/delivery-method/list 响应）。
type DeliveryMethodsPage struct {
	DeliveryMethods []DeliveryMethodInfo `json:"delivery_methods"`
	HasNext         bool                 `json:"has_next"`
	Cursor          string               `json:"cursor"`
}

type listDeliveryMethodsFilter struct {
	Status []string `json:"status,omitempty"`
}

type listDeliveryMethodsRequest struct {
	Filter  listDeliveryMethodsFilter `json:"filter"`
	Limit   int64                     `json:"limit,omitempty"`
	Cursor  string                    `json:"cursor,omitempty"`
	SortDir string                    `json:"sort_dir,omitempty"`
}

// ListDeliveryMethods 物流方式列表（/v2/delivery-method/list；v1 已于 2026-04-07 关停）。
func (c *Client) ListDeliveryMethods(ctx context.Context, p ListDeliveryMethodsParams) (*DeliveryMethodsPage, error) {
	req := listDeliveryMethodsRequest{
		Filter:  listDeliveryMethodsFilter{Status: p.Statuses},
		Limit:   p.Limit,
		Cursor:  p.Cursor,
		SortDir: p.SortDir,
	}
	if req.Limit == 0 {
		req.Limit = DefaultListLimit
	}
	var page DeliveryMethodsPage
	if err := c.do(ctx, EndpointDeliveryMethodList, pathDeliveryMethodList, req, &page); err != nil {
		return nil, err
	}
	return &page, nil
}
