// Package auth 管操作台用户与登录（总纲 §8：admin / operator 两级角色，会话存 Redis）。
//
// 用户表数据读写在本包（模型贴域走，标准档约定）；会话本身由
// internal/middleware 的 scs 管理器持有，本包只负责登录/登出/当前用户三个动作。
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/middleware"

	"github.com/alexedwards/scs/v2"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// User 操作台用户（表见 migrations：users）。
type User struct {
	ID           string    `gorm:"primaryKey;type:varchar(32)"`
	Name         string    `gorm:"type:varchar(64);uniqueIndex"`
	PasswordHash string    `gorm:"type:varchar(100)"`
	Role         string    `gorm:"type:varchar(16)"` // admin / operator
	Status       string    `gorm:"type:varchar(16)"` // active / disabled
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DelFlag      bool
}

// TableName 显式表名。
func (User) TableName() string { return "users" }

// 用户状态。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// ErrUserNotFound 用户不存在 / 已软删。
var ErrUserNotFound = errors.New("用户不存在")

// Repo 用户数据读写。
type Repo struct{ db *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ByName 按登录名查（只用未删的）。
func (r *Repo) ByName(ctx context.Context, name string) (*User, error) {
	var u User
	err := r.db.WithContext(ctx).Where("name = ? AND del_flag = ?", name, false).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ByID 按 ID 查（未删的）。
func (r *Repo) ByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create 建用户（cmd/createuser 与测试用）；密码必须是已 hash 的。
func (r *Repo) Create(ctx context.Context, u *User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// Resolve 给 internal/middleware.RequireAuth 用（签名对齐 middleware.UserResolver）：
// 每次请求回源查用户当前状态，所以禁用账号（踢人）后旧会话立即失效。
func (r *Repo) Resolve(ctx context.Context, userID string) (name, role string, active bool, err error) {
	u, err := r.ByID(ctx, userID)
	if err != nil {
		return "", "", false, err
	}
	return u.Name, u.Role, u.Status == StatusActive, nil
}

// HashPassword 生成 bcrypt 哈希。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// ---- HTTP ----

// Handler 登录相关接口。
type Handler struct {
	repo  *Repo
	sm    *scs.SessionManager
	audit *audit.Recorder
}

// NewHandler 构造。
func NewHandler(repo *Repo, sm *scs.SessionManager, rec *audit.Recorder) *Handler {
	return &Handler{repo: repo, sm: sm, audit: rec}
}

type loginReq struct {
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// RegisterPublic 注册免登录路由（挂到 /api/auth 组）。
func (h *Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.POST("/login", h.Login)
}

// RegisterAuthed 注册需登录路由（挂到已过 RequireAuth 的 /api/auth 组）。
func (h *Handler) RegisterAuthed(rg *gin.RouterGroup) {
	rg.POST("/logout", h.Logout)
	rg.GET("/me", h.Me)
}

// Login 登录：校验密码 → 换新会话 ID（防会话固定）→ 写会话。
// 失败走业务错误（HTTP 200，code=2001），不透露是用户名还是密码错。
func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()

	u, err := h.repo.ByName(ctx, req.Name)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		h.audit.Record(ctx, audit.Entry{
			Actor: req.Name, Action: "auth.login.fail", Object: "user:" + req.Name,
		})
		utils.FailWithCode(c, utils.CodeBadCred, "用户名或密码错误", nil, nil)
		return
	}
	if u.Status != StatusActive {
		h.audit.Record(ctx, audit.Entry{
			Actor: req.Name, Action: "auth.login.fail", Object: "user:" + u.ID,
			Detail: `{"reason":"disabled"}`,
		})
		utils.FailWithCode(c, utils.CodeBadCred, "账号已禁用，请联系管理员", nil, nil)
		return
	}

	// 防会话固定：重新签发会话 ID 再写 uid。
	if err := h.sm.RenewToken(ctx); err != nil {
		utils.ServerError(c, "登录失败", err)
		return
	}
	h.sm.Put(ctx, middleware.SessionKeyUserID, u.ID)

	h.audit.Record(audit.WithActor(ctx, u.Name), audit.Entry{
		Action: "auth.login", Object: "user:" + u.ID,
	})
	utils.SuccessResp(c, "ok", gin.H{"id": u.ID, "name": u.Name, "role": u.Role})
}

// Logout 销毁会话。
func (h *Handler) Logout(c *gin.Context) {
	ctx := c.Request.Context()
	name := middleware.UserNameOf(c)
	uid := middleware.UserIDOf(c)
	if err := h.sm.Destroy(ctx); err != nil {
		utils.ServerError(c, "登出失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Actor: name, Action: "auth.logout", Object: "user:" + uid})
	utils.SuccessResp(c, "已登出", nil)
}

// Me 当前用户。
func (h *Handler) Me(c *gin.Context) {
	utils.SuccessResp(c, "ok", gin.H{
		"id":   middleware.UserIDOf(c),
		"name": middleware.UserNameOf(c),
		"role": middleware.RoleOf(c),
	})
}
