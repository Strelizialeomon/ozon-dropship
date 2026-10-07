// 凭据：录入 / 脱敏展示 / 轮换（总纲 §5.4、§5.8）。
//
// 本文件定义跨份契约——S1-C 存 1688 token、S1-D 取 Ozon 凭据都走这里的
// CredentialService.Get/Put/SetExpiry（方法签名已贴 S1 父 issue #5）。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/vault"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 凭据种类（总纲 §6：store_id 空 = 企业级凭据，每种 kind 至多一行）。
const (
	KindOzonAPIKey   = "ozon_api_key"   // 每店一套（Ozon Api-Key；Client-Id 在 stores.client_id）
	KindAlibabaApp   = "alibaba_app"    // 企业级：1688 应用密钥
	KindAlibabaToken = "alibaba_token"  // 企业级：1688 买家 token（access + refresh）
)

// kindPrimaryField 每种凭据的「主密钥字段」：脱敏尾号取它。
var kindPrimaryField = map[string]string{
	KindOzonAPIKey:   "api_key",
	KindAlibabaApp:   "app_secret",
	KindAlibabaToken: "access_token",
}

// kindRequiredFields 各 kind 的必填载荷字段。
var kindRequiredFields = map[string][]string{
	KindOzonAPIKey:   {"api_key"},
	KindAlibabaApp:   {"app_key", "app_secret"},
	KindAlibabaToken: {"access_token"},
}

// tableCredentials 保险箱 AAD 里的表名（与迁移表名一致）。
const tableCredentials = "credentials"

// Credential 凭据行（表 credentials）。
type Credential struct {
	ID               string     `gorm:"primaryKey;type:varchar(32)"`
	StoreID          *string    `gorm:"type:varchar(32)"` // NULL = 企业级
	Kind             string     `gorm:"type:varchar(32)"`
	SecretEnc        []byte     `gorm:"type:blob"`
	MaskedTail       string     `gorm:"type:varchar(8)"` // 脱敏尾号（列表展示用，永不回明文）
	ExpiresAt        *time.Time `gorm:"index"`
	LastVerifiedAt   *time.Time
	RotatedAt        *time.Time
	ExpiryAlertStage int    `gorm:"column:expiry_alert_stage"` // 已告警档位（14/7/1），0 = 未告警
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DelFlag          bool
}

// TableName 显式表名。
func (Credential) TableName() string { return tableCredentials }

// ErrCredentialNotFound 凭据不存在。
var ErrCredentialNotFound = errors.New("凭据不存在")

// ErrUnknownKind 未知的凭据种类。
var ErrUnknownKind = errors.New("未知的凭据种类")

// ErrPayloadInvalid 载荷缺字段。
type ErrPayloadInvalid struct{ Reason string }

func (e *ErrPayloadInvalid) Error() string { return e.Reason }

// Decrypted 解密后的凭据（跨份契约的返回类型）。
type Decrypted struct {
	ID             string
	StoreID        string // "" = 企业级
	Kind           string
	Payload        map[string]string
	ExpiresAt      *time.Time
	LastVerifiedAt *time.Time
	RotatedAt      *time.Time
}

// Meta 脱敏后的凭据元信息（列表接口返回这个，绝不返回明文）。
type Meta struct {
	ID             string     `json:"id"`
	StoreID        string     `json:"store_id"`
	StoreName      string     `json:"store_name"`
	Kind           string     `json:"kind"`
	Masked         string     `json:"masked"` // 如 ****abcd
	ExpiresAt      *time.Time `json:"expires_at"`
	DaysLeft       *int       `json:"days_left"` // 剩余天数（站内提醒实时算，不落表）
	LastVerifiedAt *time.Time `json:"last_verified_at"`
	RotatedAt      *time.Time `json:"rotated_at"`
}

// CredentialService 凭据读写（跨份契约本体；内部经保险箱加解密）。
type CredentialService struct {
	db    *gorm.DB
	vault *vault.Vault
	audit *audit.Recorder
}

// NewCredentialService 构造。
func NewCredentialService(db *gorm.DB, v *vault.Vault, rec *audit.Recorder) *CredentialService {
	return &CredentialService{db: db, vault: v, audit: rec}
}

// Get 读明文（按「种类 + 店铺」；storeID 空串 = 企业级），并写审计。
// S1-D 取 Ozon 凭据、S1-C 取 1688 token 都调它。
func (s *CredentialService) Get(ctx context.Context, storeID, kind string) (*Decrypted, error) {
	row, err := s.find(ctx, storeID, kind)
	if err != nil {
		return nil, err
	}
	plain, err := s.vault.Decrypt(row.SecretEnc, vault.AADFor(tableCredentials, row.ID))
	if err != nil {
		if errors.Is(err, vault.ErrDisabled) {
			return nil, err
		}
		return nil, fmt.Errorf("解密凭据 %s 失败: %w", row.ID, err)
	}
	var payload map[string]string
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, fmt.Errorf("凭据 %s 载荷损坏: %w", row.ID, err)
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "credential.read",
		Object: "credential:" + row.ID,
		Detail: mustJSON(map[string]string{"store_id": deref(row.StoreID), "kind": row.Kind}),
	})
	return &Decrypted{
		ID:             row.ID,
		StoreID:        deref(row.StoreID),
		Kind:           row.Kind,
		Payload:        payload,
		ExpiresAt:      row.ExpiresAt,
		LastVerifiedAt: row.LastVerifiedAt,
		RotatedAt:      row.RotatedAt,
	}, nil
}

// Put 录入或轮换（同店同 kind 覆盖；覆盖即轮换，记 rotated_at 并复位告警档）。
// 保险箱未启用时拒绝写入（绝不落明文）。
func (s *CredentialService) Put(ctx context.Context, storeID, kind string, payload map[string]string, expiresAt *time.Time) (*Meta, error) {
	if !s.vault.Enabled() {
		return nil, vault.ErrDisabled
	}
	if err := validatePayload(kind, payload); err != nil {
		return nil, err
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	masked := maskValue(payload[kindPrimaryField[kind]])
	now := time.Now().UTC()

	existing, err := s.find(ctx, storeID, kind)
	switch {
	case err == nil:
		enc, err := s.vault.Encrypt(plain, vault.AADFor(tableCredentials, existing.ID))
		if err != nil {
			return nil, fmt.Errorf("加密失败: %w", err)
		}
		updates := map[string]any{
			"secret_enc":         enc,
			"masked_tail":        masked,
			"expires_at":         expiresAt,
			"rotated_at":         now,
			"expiry_alert_stage": 0, // 轮换开新一轮告警
		}
		if err := s.db.WithContext(ctx).Model(&Credential{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		s.audit.Record(ctx, audit.Entry{
			Action: "credential.rotate", Object: "credential:" + existing.ID,
			Detail: mustJSON(map[string]string{"store_id": storeID, "kind": kind, "masked": masked}),
		})
		return s.metaByID(ctx, existing.ID)
	case errors.Is(err, ErrCredentialNotFound):
		id := snowflake.GenStringID()
		enc, err := s.vault.Encrypt(plain, vault.AADFor(tableCredentials, id))
		if err != nil {
			return nil, fmt.Errorf("加密失败: %w", err)
		}
		row := &Credential{
			ID:        id,
			StoreID:   nullableStoreID(storeID),
			Kind:      kind,
			SecretEnc: enc,
			MaskedTail: masked,
			ExpiresAt: expiresAt,
			RotatedAt: &now,
		}
		if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
			if isDuplicate(err) {
				return nil, fmt.Errorf("同店同种类的凭据已存在（并发写入）")
			}
			return nil, err
		}
		s.audit.Record(ctx, audit.Entry{
			Action: "credential.create", Object: "credential:" + id,
			Detail: mustJSON(map[string]string{"store_id": storeID, "kind": kind, "masked": masked}),
		})
		return s.metaByID(ctx, id)
	default:
		return nil, err
	}
}

// SetExpiry 回写到期时间 / 最近校验时间（Ozon 每天读 /v1/roles、1688 续期后用）。
// 到期时间被推远（换新 key 的典型形态）时复位告警档，让新一轮 14/7/1 重新触发。
func (s *CredentialService) SetExpiry(ctx context.Context, storeID, kind string, expiresAt, lastVerifiedAt *time.Time) error {
	row, err := s.find(ctx, storeID, kind)
	if err != nil {
		return err
	}
	updates := map[string]any{}
	if expiresAt != nil {
		updates["expires_at"] = expiresAt
		if row.ExpiresAt == nil || expiresAt.After(*row.ExpiresAt) {
			updates["expiry_alert_stage"] = 0
		}
	}
	if lastVerifiedAt != nil {
		updates["last_verified_at"] = lastVerifiedAt
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&Credential{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
		return err
	}
	s.audit.Record(ctx, audit.Entry{
		Action: "credential.verify", Object: "credential:" + row.ID,
		Detail: mustJSON(map[string]string{"store_id": deref(row.StoreID), "kind": kind}),
	})
	return nil
}

// List 脱敏列表（不含任何明文；到期天数实时算）。
func (s *CredentialService) List(ctx context.Context, storeID *string) ([]Meta, error) {
	q := s.db.WithContext(ctx).Where("del_flag = ?", false)
	if storeID != nil {
		if *storeID == "" {
			q = q.Where("store_id IS NULL")
		} else {
			q = q.Where("store_id = ?", *storeID)
		}
	}
	var rows []Credential
	if err := q.Order("kind, store_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	names, err := s.shopNames(ctx)
	if err != nil {
		return nil, err
	}
	metas := make([]Meta, 0, len(rows))
	for i := range rows {
		metas = append(metas, rowToMeta(&rows[i], names))
	}
	return metas, nil
}

// find 按（店铺 + 种类）找未删行。
func (s *CredentialService) find(ctx context.Context, storeID, kind string) (*Credential, error) {
	if _, ok := kindPrimaryField[kind]; !ok {
		return nil, ErrUnknownKind
	}
	q := s.db.WithContext(ctx).Where("kind = ? AND del_flag = ?", kind, false)
	if storeID == "" {
		q = q.Where("store_id IS NULL")
	} else {
		q = q.Where("store_id = ?", storeID)
	}
	var row Credential
	err := q.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListExpiring 到期扫描用：全量未删且有 expires_at 的凭据行（不含明文）。
func (s *CredentialService) ListExpiring(ctx context.Context) ([]Credential, error) {
	var rows []Credential
	err := s.db.WithContext(ctx).
		Where("del_flag = ? AND expires_at IS NOT NULL", false).
		Find(&rows).Error
	return rows, err
}

// MarkAlertStage 记录某行已告警到哪一档（到期检查任务用）。
func (s *CredentialService) MarkAlertStage(ctx context.Context, id string, stage int) error {
	return s.db.WithContext(ctx).Model(&Credential{}).Where("id = ?", id).
		Update("expiry_alert_stage", stage).Error
}

func (s *CredentialService) metaByID(ctx context.Context, id string) (*Meta, error) {
	var row Credential
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	names, err := s.shopNames(ctx)
	if err != nil {
		return nil, err
	}
	m := rowToMeta(&row, names)
	return &m, nil
}

func (s *CredentialService) shopNames(ctx context.Context) (map[string]string, error) {
	var shops []Shop
	if err := s.db.WithContext(ctx).Where("del_flag = ?", false).Find(&shops).Error; err != nil {
		return nil, err
	}
	names := make(map[string]string, len(shops))
	for _, sp := range shops {
		names[sp.ID] = sp.Name
	}
	return names, nil
}

func rowToMeta(row *Credential, shopNames map[string]string) Meta {
	m := Meta{
		ID:             row.ID,
		StoreID:        deref(row.StoreID),
		Kind:           row.Kind,
		Masked:         row.MaskedTail,
		ExpiresAt:      row.ExpiresAt,
		LastVerifiedAt: row.LastVerifiedAt,
		RotatedAt:      row.RotatedAt,
	}
	if row.StoreID != nil {
		m.StoreName = shopNames[*row.StoreID]
	}
	if row.ExpiresAt != nil {
		days := int(math.Ceil(time.Until(*row.ExpiresAt).Hours() / 24))
		m.DaysLeft = &days
	}
	return m
}

func validatePayload(kind string, payload map[string]string) error {
	required, ok := kindRequiredFields[kind]
	if !ok {
		return ErrUnknownKind
	}
	for _, f := range required {
		if payload[f] == "" {
			return &ErrPayloadInvalid{Reason: fmt.Sprintf("凭据载荷缺字段 %s", f)}
		}
	}
	return nil
}

// maskValue 脱敏：留尾 4 位，其余打星。
func maskValue(v string) string {
	if len(v) <= 4 {
		if v == "" {
			return ""
		}
		return "****"
	}
	return "****" + v[len(v)-4:]
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullableStoreID(storeID string) *string {
	if storeID == "" {
		return nil
	}
	return &storeID
}

// ---- HTTP ----

// CredentialHandler /api/credentials。
type CredentialHandler struct {
	svc   *CredentialService
	audit *audit.Recorder
}

// NewCredentialHandler 构造。
func NewCredentialHandler(svc *CredentialService, rec *audit.Recorder) *CredentialHandler {
	return &CredentialHandler{svc: svc, audit: rec}
}

// Register 挂到已过登录校验的路由组。
func (h *CredentialHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/credentials", h.List)
	rg.POST("/credentials", h.Upsert)
}

// List GET /api/credentials?store_id=xxx（不传 = 全部）
func (h *CredentialHandler) List(c *gin.Context) {
	var storeID *string
	if v, ok := c.GetQuery("store_id"); ok {
		storeID = &v
	}
	metas, err := h.svc.List(c.Request.Context(), storeID)
	if err != nil {
		utils.ServerError(c, "查询凭据失败", err)
		return
	}
	utils.SuccessResp(c, "ok", metas)
}

type credentialReq struct {
	StoreID   string            `json:"store_id"` // 空 = 企业级
	Kind      string            `json:"kind" binding:"required"`
	Payload   map[string]string `json:"payload" binding:"required"`
	ExpiresAt *time.Time        `json:"expires_at"`
}

// Upsert POST /api/credentials（同店同 kind 覆盖 = 轮换）
func (h *CredentialHandler) Upsert(c *gin.Context) {
	var req credentialReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	meta, err := h.svc.Put(c.Request.Context(), req.StoreID, req.Kind, req.Payload, req.ExpiresAt)
	var payloadErr *ErrPayloadInvalid
	switch {
	case err == nil:
		utils.SuccessResp(c, "已保存", meta)
	case errors.Is(err, vault.ErrDisabled):
		utils.FailWithCode(c, utils.CodeVaultUnready, "保险箱未启用，拒绝保存凭据", nil, nil)
	case errors.Is(err, ErrUnknownKind):
		utils.FailWithCode(c, utils.CodeValidate, "未知的凭据种类", err, nil)
	case errors.As(err, &payloadErr):
		utils.FailWithCode(c, utils.CodeValidate, payloadErr.Reason, nil, nil)
	default:
		utils.ServerError(c, "保存凭据失败", err)
	}
}
