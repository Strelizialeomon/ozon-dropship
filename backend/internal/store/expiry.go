// 凭据到期告警（总纲 §5.8）：Ozon 每天读 /v1/roles 刷新到期时间；
// 到期前 14 / 7 / 1 天各告警一次（站内 + 飞书），同一档不重复。
//
// 站内提醒 = 系统/凭据页实时算剩余天数（List 的 days_left），不落表；
// 飞书走 infra/notify（去重 + 按分钟合并）。已告警档位记在 credentials.expiry_alert_stage。
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/notify"

	"github.com/hibiken/asynq"
)

// TaskTypeExpiryCheck 到期检查任务类型（cmd/api 注册每天北京时间 09:00）。
const TaskTypeExpiryCheck = "store:expiry_check"

// ExpiryAlertThresholds 告警档位（天），升序——stageForDays 取「最紧的够档」。
var ExpiryAlertThresholds = []int{1, 7, 14}

// stageForDays 当前应处的告警档位；0 = 距到期还早（超过最大档）。
func stageForDays(days float64) int {
	for _, th := range ExpiryAlertThresholds {
		if days <= float64(th) {
			return th
		}
	}
	return 0
}

// kindDisplayName 告警文案里的种类名。
var kindDisplayName = map[string]string{
	KindOzonAPIKey:   "Ozon Api-Key",
	KindAlibabaApp:   "1688 应用密钥",
	KindAlibabaToken: "1688 买家 token",
}

// OzonRolesFetcher 「读某店 Ozon key 到期时间」的小接口——接口放使用方（包依赖 ADR）：
// S1-B 的 ozon 客户端在装配层适配它（签名已贴 S1 父 issue #5）。
type OzonRolesFetcher interface {
	// FetchExpiry 调 Ozon /v1/roles 返回该 key 的到期时间。
	FetchExpiry(ctx context.Context) (expiresAt time.Time, err error)
}

// OzonFetcherFactory 装配层注入：给定店铺与解密后的凭据，造一个 fetcher。
// S1-B 未合并前为 nil——跳过「读 Ozon 到期时间」，本地 expires_at 照常告警。
type OzonFetcherFactory func(shop Shop, cred *Decrypted) (OzonRolesFetcher, error)

// ExpiryChecker 到期检查任务（asynq handler）。
type ExpiryChecker struct {
	creds  *CredentialService
	shops  *Repo
	notify *notify.Notifier
	ozon   OzonFetcherFactory
}

// NewExpiryChecker 构造。ozon 可为 nil（S1-B 合并后在装配层接上）。
func NewExpiryChecker(creds *CredentialService, shops *Repo, n *notify.Notifier, ozon OzonFetcherFactory) *ExpiryChecker {
	return &ExpiryChecker{creds: creds, shops: shops, notify: n, ozon: ozon}
}

// Handle asynq 任务入口。
func (e *ExpiryChecker) Handle(ctx context.Context, _ *asynq.Task) error {
	e.refreshFromOzon(ctx)
	e.checkThresholds(ctx)
	return nil
}

// refreshFromOzon 逐店读 Ozon key 到期时间并回写。
func (e *ExpiryChecker) refreshFromOzon(ctx context.Context) {
	if e.ozon == nil {
		return // S1-B 未接上：跳过；本地已存的 expires_at 照常参与告警
	}
	shops, err := e.shops.List(ctx)
	if err != nil {
		logger.Errorf("[expiry] 列店铺失败: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, shop := range shops {
		cred, err := e.creds.Get(ctx, shop.ID, KindOzonAPIKey)
		if errors.Is(err, ErrCredentialNotFound) {
			continue
		}
		if err != nil {
			logger.Errorf("[expiry] 读店铺 %s 凭据失败: %v", shop.Name, err)
			continue
		}
		fetcher, err := e.ozon(shop, cred)
		if err != nil {
			logger.Errorf("[expiry] 构造店铺 %s 的 Ozon 读取器失败: %v", shop.Name, err)
			continue
		}
		expiresAt, err := fetcher.FetchExpiry(ctx)
		if err != nil {
			// 读不到就告警（key 可能已失效），按店去重避免天天刷屏。
			e.notify.Notify("ozon_roles:"+shop.ID, "Ozon 密钥读取失败",
				fmt.Sprintf("店铺 %s 读取 /v1/roles 失败：%v", shop.Name, err))
			continue
		}
		if err := e.creds.SetExpiry(ctx, shop.ID, KindOzonAPIKey, &expiresAt, &now); err != nil {
			logger.Errorf("[expiry] 回写店铺 %s 到期时间失败: %v", shop.Name, err)
		}
	}
}

// checkThresholds 判定档位并发告警；跨档才发，同一档不重复。
func (e *ExpiryChecker) checkThresholds(ctx context.Context) {
	rows, err := e.creds.ListExpiring(ctx)
	if err != nil {
		logger.Errorf("[expiry] 列凭据失败: %v", err)
		return
	}
	names := map[string]string{}
	if shops, err := e.shops.List(ctx); err == nil {
		for _, sp := range shops {
			names[sp.ID] = sp.Name
		}
	}

	now := time.Now().UTC()
	for i := range rows {
		row := &rows[i]
		days := row.ExpiresAt.Sub(now).Hours() / 24
		stage := stageForDays(days)

		if stage == 0 {
			// 轮换后到期时间推远：复位，下次接近时重新触发。
			if row.ExpiryAlertStage != 0 {
				if err := e.creds.MarkAlertStage(ctx, row.ID, 0); err != nil {
					logger.Errorf("[expiry] 复位告警档失败 credential=%s: %v", row.ID, err)
				}
			}
			continue
		}
		// 已告警过这一档或更紧的档 → 跳过（同一档不重复）。
		if row.ExpiryAlertStage != 0 && stage >= row.ExpiryAlertStage {
			continue
		}

		scope := "企业级凭据"
		if row.StoreID != nil {
			scope = "店铺 " + names[*row.StoreID]
			if names[*row.StoreID] == "" {
				scope = "店铺 " + *row.StoreID
			}
		}
		kindName := kindDisplayName[row.Kind]
		if kindName == "" {
			kindName = row.Kind
		}
		left := int(days)
		if days > 0 && float64(left) < days {
			left++ // 向上取整，让「剩 1 天」不显示成 0
		}
		text := fmt.Sprintf("%s 的 %s 将于 %s 到期（剩 %d 天），请及时轮换。",
			scope, kindName, row.ExpiresAt.Format("2006-01-02 15:04"), left)

		// 去重键带到期日：轮换换了新 key（到期日变）时，同一档要能重新提醒；
		// 到期日没变的重发才被吞（数据库里的告警档位是第一道闸，这里是第二道）。
		dedupeKey := "credential:" + row.ID + ":stage:" + strconv.Itoa(stage) +
			":exp:" + strconv.FormatInt(row.ExpiresAt.Unix(), 10)
		e.notify.Notify(dedupeKey, "凭据到期告警", text)
		logger.Warnf("[expiry] %s（档位 %d 天）", text, stage)

		if err := e.creds.MarkAlertStage(ctx, row.ID, stage); err != nil {
			logger.Errorf("[expiry] 记录告警档失败 credential=%s: %v", row.ID, err)
		}
	}
}
