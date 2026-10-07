package ozon

import (
	"context"
)

// Role 一个密钥角色及其可调方法（官方 /v1/roles 响应 roles[] 的元素）。
type Role struct {
	Name    string   `json:"name"`
	Methods []string `json:"methods"`
}

// Roles 密钥角色信息（官方 /v1/roles 响应）。
type Roles struct {
	// ExpiresAt 密钥到期时间（S1-A 到期检查回写用；总纲 §5.8）。
	ExpiresAt Time `json:"expires_at"`
	// Roles 角色列表与各自可调方法。
	Roles []Role `json:"roles"`
}

// GetRoles 读密钥角色与到期时间（/v1/roles）。
// S1-A 的到期检查（store.OzonRolesFetcher）在装配层适配本方法。
func (c *Client) GetRoles(ctx context.Context) (*Roles, error) {
	var resp Roles
	if err := c.do(ctx, EndpointRoles, pathRoles, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
