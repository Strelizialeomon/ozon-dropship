// S1-B 装配：把 internal/ozon 接上 S1-A 的凭据到期检查与渠道限流器。
//
// 依赖方向（ADR-20261007-go-package-deps）：internal/ozon 不 import store——
// store 侧定义接口（store.OzonRolesFetcher），适配放这里（组合根）。
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/config"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/ozon"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"
)

// newRateLimiter 渠道限流器（进程内单例，全通道共享；跨份契约见 S1 父 issue #5）。
// 重试参数复用总纲 §5.5 的默认值（S1-A 落在 queue 配置段；HTTP 层与任务层暂时同值）。
func newRateLimiter(cfg *config.AppConfig) *ratelimit.Registry {
	return ratelimit.New(ratelimit.Config{
		Ozon: ratelimit.SubjectConfig{
			DefaultRPS: cfg.RateLimit.Ozon.DefaultRPS,
			Endpoints:  cfg.RateLimit.Ozon.Endpoints,
		},
		Alibaba: ratelimit.SubjectConfig{
			DefaultRPS: cfg.RateLimit.Alibaba.DefaultRPS,
			Endpoints:  cfg.RateLimit.Alibaba.Endpoints,
		},
	}, cfg.Queue.RetryMaxRetries, time.Duration(cfg.Queue.RetryBackoffSeconds)*time.Second)
}

// ozonRolesFetcherFactory 造 store 侧要的工厂：给定店铺与解密凭据，
// 返回读 /v1/roles 到期时间的 fetcher（总纲 §5.8）。
func ozonRolesFetcherFactory(limiter *ratelimit.Registry) store.OzonFetcherFactory {
	return func(shop store.Shop, cred *store.Decrypted) (store.OzonRolesFetcher, error) {
		if shop.ClientID == "" {
			return nil, fmt.Errorf("店铺 %s 未配 Client-Id", shop.Name)
		}
		apiKey := cred.Payload["api_key"]
		if apiKey == "" {
			return nil, fmt.Errorf("店铺 %s 的 Ozon 凭据缺 api_key 字段", shop.Name)
		}
		return &ozonRolesFetcher{client: ozon.New(limiter, ozon.Options{
			ClientID: shop.ClientID,
			APIKey:   apiKey,
			Subject:  shop.ID, // 限流桶按店铺（每店一个 Client-Id）
		})}, nil
	}
}

// ozonRolesFetcher 把 ozon.Client.GetRoles 适配成 store.OzonRolesFetcher。
type ozonRolesFetcher struct{ client *ozon.Client }

// FetchExpiry 调 /v1/roles 返回该 key 的到期时间。
func (f *ozonRolesFetcher) FetchExpiry(ctx context.Context) (time.Time, error) {
	roles, err := f.client.GetRoles(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if roles.ExpiresAt.IsZero() {
		return time.Time{}, fmt.Errorf("响应缺 expires_at（Ozon /v1/roles）")
	}
	return roles.ExpiresAt.Time, nil
}
