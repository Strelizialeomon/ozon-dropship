// Package store 是「店铺与凭据域」（总纲 §4）：店铺增删改查、凭据录入/脱敏/轮换、
// 凭据到期告警，以及系统状态接口。
//
// ⚠️ 包名 store 指「店铺」，不是数据层（数据读写直接在各 resource 文件里）。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Shop 店铺（表 stores）。字段与总纲 §6 对齐。
type Shop struct {
	ID                  string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Name                string     `gorm:"type:varchar(128);uniqueIndex" json:"name"`
	Mode                string     `gorm:"type:varchar(16)" json:"mode"` // rfbs / fbp / local
	ClientID            string     `gorm:"column:client_id;type:varchar(64)" json:"client_id"`
	Currency            string     `gorm:"type:char(3)" json:"currency"`
	DefaultRelayPointID *string    `gorm:"type:varchar(32)" json:"default_relay_point_id"`
	PushEnabled         bool       `json:"push_enabled"`
	ShipEarly           bool       `json:"ship_early"`
	LastSyncAt          *time.Time `json:"last_sync_at"` // 最近一次同步成功（S1-D 写、系统状态页读）
	Status              string     `gorm:"type:varchar(16)" json:"status"` // active / paused
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DelFlag             bool       `json:"del_flag"`
}

// TableName 显式表名。
func (Shop) TableName() string { return "stores" }

// 店铺状态 / 发货模式（与迁移里的注释一致）。
const (
	ShopStatusActive = "active"
	ShopStatusPaused = "paused"

	ModeRFBS  = "rfbs"
	ModeFBP   = "fbp"
	ModeLocal = "local"
)

// ErrShopNotFound 店铺不存在 / 已软删。
var ErrShopNotFound = errors.New("店铺不存在")

// Repo 店铺数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// DB 暴露底层连接（本包内其他文件与装配层用）。
func (r *Repo) DB() *gorm.DB { return r.db }

// List 全部未删店铺。
func (r *Repo) List(ctx context.Context) ([]Shop, error) {
	var rows []Shop
	if err := r.db.WithContext(ctx).Where("del_flag = ?", false).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ByID 按 ID 取。
func (r *Repo) ByID(ctx context.Context, id string) (*Shop, error) {
	var s Shop
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShopNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create 建店（重名返回 gorm 唯一键错误，handler 转 1002）。
func (r *Repo) Create(ctx context.Context, s *Shop) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// Update 保存店铺的可改字段。
// 带 del_flag=false 条件（不用 Save 存整行）：与删除并发时，Save 会把 del_flag
// 一起写回 false，把刚删掉的店「写活」；条件更新 0 行即返回 ErrShopNotFound。
func (r *Repo) Update(ctx context.Context, s *Shop) error {
	res := r.db.WithContext(ctx).Model(&Shop{}).
		Where("id = ? AND del_flag = ?", s.ID, false).
		Updates(map[string]any{
			"name":                   s.Name,
			"mode":                   s.Mode,
			"client_id":              s.ClientID,
			"currency":               s.Currency,
			"default_relay_point_id": s.DefaultRelayPointID,
			"push_enabled":           s.PushEnabled,
			"ship_early":             s.ShipEarly,
			"status":                 s.Status,
			"updated_at":             time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrShopNotFound
	}
	return nil
}

// SoftDelete 软删。
func (r *Repo) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&Shop{}).Where("id = ?", id).Update("del_flag", true).Error
}

// ---- HTTP ----

// Handler 店铺接口（/api/stores）。
type Handler struct {
	repo  *Repo
	audit *audit.Recorder
}

// NewHandler 构造。
func NewHandler(repo *Repo, rec *audit.Recorder) *Handler {
	return &Handler{repo: repo, audit: rec}
}

// Register 挂到已过登录校验的路由组。
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/stores", h.List)
	rg.POST("/stores", h.Create)
	rg.GET("/stores/:id", h.Get)
	rg.PUT("/stores/:id", h.Update)
	rg.DELETE("/stores/:id", h.Delete)
}

type shopReq struct {
	Name                string  `json:"name" binding:"required"`
	Mode                string  `json:"mode" binding:"required,oneof=rfbs fbp local"`
	ClientID            string  `json:"client_id"`
	Currency            string  `json:"currency"`
	DefaultRelayPointID *string `json:"default_relay_point_id"`
	PushEnabled         bool    `json:"push_enabled"`
	ShipEarly           bool    `json:"ship_early"`
	Status              string  `json:"status" binding:"omitempty,oneof=active paused"`
}

// List GET /api/stores
func (h *Handler) List(c *gin.Context) {
	rows, err := h.repo.List(c.Request.Context())
	if err != nil {
		utils.ServerError(c, "查询店铺失败", err)
		return
	}
	utils.SuccessResp(c, "ok", rows)
}

// Get GET /api/stores/:id
func (h *Handler) Get(c *gin.Context) {
	s, err := h.repo.ByID(c.Request.Context(), c.Param("id"))
	if errors.Is(err, ErrShopNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "店铺不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询店铺失败", err)
		return
	}
	utils.SuccessResp(c, "ok", s)
}

// Create POST /api/stores
func (h *Handler) Create(c *gin.Context) {
	var req shopReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	s := &Shop{
		ID:                  snowflake.GenStringID(),
		Name:                req.Name,
		Mode:                req.Mode,
		ClientID:            req.ClientID,
		Currency:            orDefault(req.Currency, "CNY"),
		DefaultRelayPointID: req.DefaultRelayPointID,
		PushEnabled:         req.PushEnabled,
		ShipEarly:           req.ShipEarly,
		Status:              orDefault(req.Status, ShopStatusActive),
	}
	if err := h.repo.Create(ctx, s); err != nil {
		if isDuplicate(err) {
			utils.FailWithCode(c, utils.CodeConflict, "店铺名已存在", nil, nil)
			return
		}
		utils.ServerError(c, "创建店铺失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "store.create", Object: "store:" + s.ID, Detail: mustJSON(s)})
	utils.SuccessResp(c, "已创建", s)
}

// Update PUT /api/stores/:id
func (h *Handler) Update(c *gin.Context) {
	var req shopReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	s, err := h.repo.ByID(ctx, c.Param("id"))
	if errors.Is(err, ErrShopNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "店铺不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询店铺失败", err)
		return
	}
	before := mustJSON(s)
	s.Name = req.Name
	s.Mode = req.Mode
	s.ClientID = req.ClientID
	s.Currency = orDefault(req.Currency, "CNY")
	s.DefaultRelayPointID = req.DefaultRelayPointID
	s.PushEnabled = req.PushEnabled
	s.ShipEarly = req.ShipEarly
	s.Status = orDefault(req.Status, s.Status)

	if err := h.repo.Update(ctx, s); err != nil {
		switch {
		case errors.Is(err, ErrShopNotFound):
			// 加载后、保存前被别人删了。
			utils.FailWithCode(c, utils.CodeNotFound, "店铺不存在（可能刚被删除）", nil, nil)
		case isDuplicate(err):
			utils.FailWithCode(c, utils.CodeConflict, "店铺名已存在", nil, nil)
		default:
			utils.ServerError(c, "更新店铺失败", err)
		}
		return
	}
	// 回读一次：拿更新后的 updated_at，响应不糊弄。
	if fresh, err := h.repo.ByID(ctx, s.ID); err == nil {
		s = fresh
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "store.update", Object: "store:" + s.ID,
		Detail: diffJSON(before, mustJSON(s)),
	})
	utils.SuccessResp(c, "已更新", s)
}

// Delete DELETE /api/stores/:id（软删）
func (h *Handler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	s, err := h.repo.ByID(ctx, c.Param("id"))
	if errors.Is(err, ErrShopNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "店铺不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询店铺失败", err)
		return
	}
	if err := h.repo.SoftDelete(ctx, s.ID); err != nil {
		utils.ServerError(c, "删除店铺失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "store.delete", Object: "store:" + s.ID, Detail: beforeNote(s)})
	utils.SuccessResp(c, "已删除", nil)
}

// ---- 小工具 ----

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func beforeNote(s *Shop) string {
	b, _ := json.Marshal(map[string]string{"name": s.Name})
	return string(b)
}

func diffJSON(before, after string) string {
	b, _ := json.Marshal(map[string]string{"before": before, "after": after})
	return string(b)
}

// isDuplicate 判断唯一键冲突（db.Init 开了 TranslateError，驱动会翻译成 ErrDuplicatedKey；
// 兜底再看错误文本——迁移里的生成列唯一键在某些驱动版本下翻译不全）。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(err.Error(), "Duplicate entry")
}
