// Package audit 是写 audit_logs 的统一入口（总纲 §5.11）：
// 谁（用户或系统任务）、做了什么、对哪个对象、前后差异、何时。
// 手动与自动动作一视同仁；凭据读取与轮换必记。
package audit

import (
	"context"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"

	"gorm.io/gorm"
)

// newID 审计行主键（雪花 ID，与其他表一致）。
func newID() string { return snowflake.GenStringID() }

// AuditLog 审计日志行（append-only，表见 migrations）。
// 与业务模型的差异：本表只追加、不软删，故无 del_flag / updated_at
// （见 backend/ARCHITECTURE.md「本项目约定」）。
type AuditLog struct {
	ID     string    `gorm:"primaryKey;type:varchar(32)"`
	Actor  string    `gorm:"type:varchar(128);index"`
	Action string    `gorm:"type:varchar(64);index"`
	Object string    `gorm:"type:varchar(128)"`
	Detail string    `gorm:"type:text"`
	At     time.Time `gorm:"index"`
}

// TableName 显式表名（标准档模型约定）。
func (AuditLog) TableName() string { return "audit_logs" }

// Entry 一条审计。
type Entry struct {
	Actor  string // 用户 name/id，或 "system"
	Action string // 如 store.create / credential.rotate / credential.read
	Object string // 如 store:123 / credential:456
	Detail string // JSON 字符串（前后差异等）
}

// Recorder 审计记录器。
type Recorder struct {
	db *gorm.DB
}

// New 构造记录器。
func New(db *gorm.DB) *Recorder { return &Recorder{db: db} }

// Record 写一条审计。写失败只记日志、不阻断业务——但会明显出现在日志里，
// 因为审计缺失本身就是要人知道的事故。
func (r *Recorder) Record(ctx context.Context, e Entry) {
	if r == nil || r.db == nil {
		return
	}
	if e.Actor == "" {
		e.Actor = ActorFrom(ctx)
	}
	row := AuditLog{
		ID:     newID(),
		Actor:  e.Actor,
		Action: e.Action,
		Object: e.Object,
		Detail: e.Detail,
		At:     time.Now().UTC(),
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		logger.Errorf("[audit] 写审计失败 action=%s object=%s: %v", e.Action, e.Object, err)
	}
}

// ---- actor 透传：middleware 把登录用户写进 ctx，业务与 infra 统一从这里取 ----

type actorKey struct{}

// WithActor 把操作者写进 context。
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom 取出操作者；没有就回 "system"（后台任务）。
func ActorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorKey{}).(string); ok && v != "" {
		return v
	}
	return "system"
}
