// 异常池（exceptions 表，总纲 §5.2 / §6）。
//
// 「同一对象同一 code 未处理时去重」做成了数据库唯一键（open_key 生成列）：
// 插入撞 1062 即表示已有未处理的同码异常，直接当成功。
//
// 异常池放 order 包：总纲 §5.2 把「订单状态机 + 异常队列」并列为一条机制，
// 且依赖链上 order 在 purchase / shipment 之下，两个包都能往上调（ADR-20261007-go-package-deps）。
package order

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 异常对象类型（ref_type）。
const (
	RefOrder        = "order"
	RefPurchaseTask = "purchase_task"
	RefShipment     = "shipment"
)

// 异常码（总纲 §5.2 清单，S1 部分；每一码一个常量，写库前不许拼字符串）。
const (
	CodePurchaseTimeout     = "purchase_timeout"      // 超时未采购
	CodeAlibabaOrderFailed  = "alibaba_order_failed"  // 1688 下单失败
	CodeAddressInvalid      = "address_invalid"       // 地址校验失败
	CodeShipDeadlineNear    = "ship_deadline_near"    // 发货截止临近
	CodeDomesticStalled     = "domestic_stalled"      // 国内段停滞（下单后 72 小时无更新）
	CodeRelayStalled        = "relay_stalled"         // 中转点停滞（签收后 24 小时未交运）
	CodeShipFailed          = "ship_failed"           // 备货返回成功但 substatus = ship_failed
	CodeArbitration         = "arbitration"           // 仲裁
	CodeUnknownStatus       = "unknown_status"        // Ozon 表外状态
	CodeHybridTpl           = "hybrid_tpl"            // hybrid 物流类型（S1 不涉及）
	CodeUnknownTpl          = "unknown_tpl"           // tpl_integration_type 不在官方五个取值内
	CodeOfferMappingMissing = "offer_mapping_missing" // 没有可用的货源映射（自定细节，见 PR 说明）
)

// 异常状态。
const (
	ExceptionOpen     = "open"
	ExceptionResolved = "resolved"
)

// Exception 异常行（表 exceptions）。
type Exception struct {
	ID        string     `gorm:"primaryKey;type:varchar(32)" json:"id"`
	RefType   string     `gorm:"column:ref_type;type:varchar(24)" json:"ref_type"`
	RefID     string     `gorm:"column:ref_id;type:varchar(32)" json:"ref_id"`
	Code      string     `gorm:"type:varchar(64)" json:"code"`
	Detail    *string    `gorm:"type:text" json:"detail"`
	Status    string     `gorm:"type:varchar(16)" json:"status"`
	HandledBy *string    `gorm:"column:handled_by;type:varchar(64)" json:"handled_by"`
	HandledAt *time.Time `gorm:"column:handled_at" json:"handled_at"`
	Note      *string    `gorm:"type:text" json:"note"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DelFlag   bool       `json:"del_flag"`
}

// TableName 显式表名。
func (Exception) TableName() string { return "exceptions" }

// ErrExceptionNotFound 异常不存在。
var ErrExceptionNotFound = errors.New("异常不存在")

// Notifier 异常告警出口（*notify.Notifier 满足；装配层注入）。
// 总纲 §5.2 的标题就是「异常判定（**自动进池 + 通知**）」——只进池不告警，
// 表外状态这类问题会被静默吞掉，等人在池子里翻到已经晚了。
type Notifier interface {
	Notify(dedupeKey, title, text string)
}

// Exceptions 异常池读写。
type Exceptions struct {
	db     *gorm.DB
	audit  *audit.Recorder
	notify Notifier
}

// NewExceptions 构造（异常写入同样留审计，总纲 §5.11「自动与手动动作一视同仁」）。
func NewExceptions(db *gorm.DB) *Exceptions { return &Exceptions{db: db, audit: audit.New(db)} }

// SetNotifier 注入告警出口；不注 = 只进池不告警（测试与不配飞书的部署）。
func (e *Exceptions) SetNotifier(n Notifier) { e.notify = n }

// Raise 报一条异常。同对象同 code 已有未处理行时静默去重（返回 nil）。
// detail 里放人能看懂的上下文（单号、金额、原始状态值等）。
func (e *Exceptions) Raise(ctx context.Context, refType, refID, code, detail string) error {
	if refID == "" || code == "" {
		return errors.New("异常缺少对象或码")
	}
	d := detail
	row := &Exception{
		ID:      snowflake.GenStringID(),
		RefType: refType,
		RefID:   refID,
		Code:    code,
		Detail:  &d,
		Status:  ExceptionOpen,
	}
	err := e.db.WithContext(ctx).Create(row).Error
	if err != nil {
		if isDuplicate(err) {
			return nil // 已有未处理的同码异常：去重
		}
		return err
	}
	e.audit.Record(ctx, audit.Entry{
		Action: "exception.raise",
		Object: "exception:" + row.ID,
		Detail: mustJSON(map[string]string{"ref_type": refType, "ref_id": refID, "code": code, "detail": detail}),
	})
	if e.notify != nil {
		// 去重键 = 对象 + 码：同一对象同一码的重复告警由 notify 自己的窗口合并。
		e.notify.Notify("exception:"+refType+":"+refID+":"+code, "履约异常",
			fmt.Sprintf("[%s] %s/%s：%s", code, refType, refID, detail))
	}
	return nil
}

// OpenCount 某对象当前未处理的异常数（接口与扫描用）。
func (e *Exceptions) OpenCount(ctx context.Context, refType, refID string) (int64, error) {
	var n int64
	err := e.db.WithContext(ctx).Model(&Exception{}).
		Where("ref_type = ? AND ref_id = ? AND status = ? AND del_flag = ?", refType, refID, ExceptionOpen, false).
		Count(&n).Error
	return n, err
}

// ExceptionFilter 列表筛选。
type ExceptionFilter struct {
	RefType  string
	RefID    string
	Code     string
	Status   string
	Page     int
	PageSize int
}

// List 分页列表（默认只看未处理）。
func (e *Exceptions) List(ctx context.Context, f ExceptionFilter) ([]Exception, int64, error) {
	q := e.db.WithContext(ctx).Model(&Exception{}).Where("del_flag = ?", false)
	if f.RefType != "" {
		q = q.Where("ref_type = ?", f.RefType)
	}
	if f.RefID != "" {
		q = q.Where("ref_id = ?", f.RefID)
	}
	if f.Code != "" {
		q = q.Where("code = ?", f.Code)
	}
	if f.Status == "" {
		f.Status = ExceptionOpen
	}
	if f.Status != "all" {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(f.Page, f.PageSize)
	var rows []Exception
	err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// ByID 按 ID 取。
func (e *Exceptions) ByID(ctx context.Context, id string) (*Exception, error) {
	var row Exception
	err := e.db.WithContext(ctx).Where("id = ? AND del_flag = ?", id, false).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExceptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Resolve 手动处理：置 resolved 并记处理人与备注。已处理的再点一次不报错。
func (e *Exceptions) Resolve(ctx context.Context, id, note, actor string) (*Exception, error) {
	row, err := e.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status == ExceptionResolved {
		return row, nil
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":     ExceptionResolved,
		"handled_by": actor,
		"handled_at": now,
		"updated_at": now,
	}
	if note != "" {
		updates["note"] = note
	}
	if err := e.db.WithContext(ctx).Model(&Exception{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return e.ByID(ctx, id)
}

// ---- HTTP ----

// ExceptionHandler /api/exceptions（异常池）。
type ExceptionHandler struct {
	exc   *Exceptions
	audit *audit.Recorder
}

// NewExceptionHandler 构造。
func NewExceptionHandler(exc *Exceptions, rec *audit.Recorder) *ExceptionHandler {
	return &ExceptionHandler{exc: exc, audit: rec}
}

// Register 挂到已过登录校验的路由组。
func (h *ExceptionHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/exceptions", h.List)
	rg.POST("/exceptions/:id/resolve", h.Resolve)
}

// List GET /api/exceptions?ref_type=&ref_id=&code=&status=&page=&page_size=
func (h *ExceptionHandler) List(c *gin.Context) {
	rows, total, err := h.exc.List(c.Request.Context(), ExceptionFilter{
		RefType:  c.Query("ref_type"),
		RefID:    c.Query("ref_id"),
		Code:     c.Query("code"),
		Status:   c.Query("status"),
		Page:     parseInt(c.Query("page")),
		PageSize: parseInt(c.Query("page_size")),
	})
	if err != nil {
		utils.ServerError(c, "查询异常池失败", err)
		return
	}
	utils.SuccessResp(c, "ok", gin.H{"total": total, "items": rows})
}

// Resolve POST /api/exceptions/:id/resolve {"note":"..."}
func (h *ExceptionHandler) Resolve(c *gin.Context) {
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 备注可以不带，body 为空也算合法
		req.Note = ""
	}
	ctx := c.Request.Context()
	actor := audit.ActorFrom(ctx)
	row, err := h.exc.Resolve(ctx, c.Param("id"), strings.TrimSpace(req.Note), actor)
	if errors.Is(err, ErrExceptionNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "异常不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "处理异常失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "exception.resolve", Object: "exception:" + row.ID,
		Detail: mustJSON(map[string]string{"ref_type": row.RefType, "ref_id": row.RefID, "code": row.Code, "note": req.Note}),
	})
	utils.SuccessResp(c, "已处理", row)
}

// isDuplicate 唯一键冲突（open_key 生成列；与 store / catalog 同款兜底）。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "Duplicate entry")
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func parseInt(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
