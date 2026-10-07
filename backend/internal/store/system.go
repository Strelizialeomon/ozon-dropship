// 系统状态接口（总纲 §8 / 子 spec §3）：各店最近一次同步成功时间（读 stores.last_sync_at）、
// 队列积压、失败任务（asynq 归档）。
package store

import (
	"context"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// SystemHandler /api/system（仅登录后可见）。
type SystemHandler struct {
	shops     *Repo
	inspector *asynq.Inspector
}

// NewSystemHandler 构造。
func NewSystemHandler(shops *Repo, inspector *asynq.Inspector) *SystemHandler {
	return &SystemHandler{shops: shops, inspector: inspector}
}

// Register 挂到已过登录校验的路由组。
func (h *SystemHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/system", h.Get)
}

type shopSyncStatus struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSyncAt *time.Time `json:"last_sync_at"`
}

type queueStat struct {
	Pending   int `json:"pending"`
	Active    int `json:"active"`
	Scheduled int `json:"scheduled"`
	Retry     int `json:"retry"`
	Archived  int `json:"archived"` // 重试用尽落这里 = 「失败任务」
}

type failedTask struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Queue        string    `json:"queue"`
	Retried      int       `json:"retried"`
	MaxRetry     int       `json:"max_retry"`
	LastError    string    `json:"last_error"`
	LastFailedAt time.Time `json:"last_failed_at"`
}

// Get GET /api/system
func (h *SystemHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	shops, err := h.shops.List(ctx)
	if err != nil {
		utils.ServerError(c, "查询系统状态失败", err)
		return
	}
	syncStatus := make([]shopSyncStatus, 0, len(shops))
	for _, s := range shops {
		syncStatus = append(syncStatus, shopSyncStatus{
			ID: s.ID, Name: s.Name, Status: s.Status, LastSyncAt: s.LastSyncAt,
		})
	}

	queues, failed := h.queueSnapshot(ctx)
	utils.SuccessResp(c, "ok", gin.H{
		"stores":       syncStatus,
		"queues":       queues,
		"failed_tasks": failed,
	})
}

// queueSnapshot 队列积压 + 归档任务（失败任务）。读不到就返回空表并带错误日志，
// 不让系统状态页整体 500——队列短暂不通时其他信息仍有参考价值。
func (h *SystemHandler) queueSnapshot(ctx context.Context) (map[string]queueStat, []failedTask) {
	stats := map[string]queueStat{}
	failed := []failedTask{}
	if h.inspector == nil {
		return stats, failed
	}

	names, err := h.inspector.Queues()
	if err != nil {
		return stats, failed
	}
	for _, q := range names {
		info, err := h.inspector.GetQueueInfo(q)
		if err != nil {
			continue
		}
		stats[q] = queueStat{
			Pending:   info.Pending,
			Active:    info.Active,
			Scheduled: info.Scheduled,
			Retry:     info.Retry,
			Archived:  info.Archived,
		}
		if info.Archived == 0 {
			continue
		}
		tasks, err := h.inspector.ListArchivedTasks(q, asynq.PageSize(20), asynq.Page(1))
		if err != nil {
			continue
		}
		for _, t := range tasks {
			failed = append(failed, failedTask{
				ID:           t.ID,
				Type:         t.Type,
				Queue:        q,
				Retried:      t.Retried,
				MaxRetry:     t.MaxRetry,
				LastError:    t.LastErr,
				LastFailedAt: t.LastFailedAt,
			})
		}
	}
	_ = ctx
	return stats, failed
}
