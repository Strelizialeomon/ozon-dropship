// 采购任务台接口（总纲 §8：执行 / 转人工 / 备料单 / 回填）。
package purchase

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// Handler /api/purchase-tasks。
type Handler struct {
	repo  *Repo
	svc   *Service
	audit *audit.Recorder
}

// NewHandler 构造。
func NewHandler(repo *Repo, svc *Service, rec *audit.Recorder) *Handler {
	return &Handler{repo: repo, svc: svc, audit: rec}
}

// Register 挂到已过登录校验的路由组。
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/purchase-tasks", h.List)
	rg.GET("/purchase-tasks/:id", h.Get)
	rg.POST("/purchase-tasks/:id/execute", h.Execute)
	rg.POST("/purchase-tasks/:id/convert-manual", h.ConvertManual)
	rg.GET("/purchase-tasks/:id/material-sheet", h.MaterialSheet)
	rg.POST("/purchase-tasks/:id/mark-paid", h.MarkPaid)
	rg.POST("/purchase-tasks/:id/fill-back", h.FillBack)
}

// List GET /api/purchase-tasks?order_id=&store_id=&status=&executor_type=&assignee=
func (h *Handler) List(c *gin.Context) {
	rows, total, err := h.repo.ListTasks(c.Request.Context(), TaskFilter{
		OrderID:      c.Query("order_id"),
		StoreID:      c.Query("store_id"),
		Status:       c.Query("status"),
		ExecutorType: c.Query("executor_type"),
		Assignee:     c.Query("assignee"),
		Page:         queryInt(c, "page"),
		PageSize:     queryInt(c, "page_size"),
	})
	if err != nil {
		utils.ServerError(c, "查询采购任务失败", err)
		return
	}
	utils.SuccessResp(c, "ok", gin.H{"total": total, "items": rows})
}

// Get GET /api/purchase-tasks/:id（含采购单与备料单）
func (h *Handler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	task, err := h.repo.TaskByID(ctx, c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询采购任务失败", err)
		return
	}
	po, err := h.repo.PurchaseOrderByTask(ctx, task.ID)
	if err != nil {
		utils.ServerError(c, "查询采购单失败", err)
		return
	}
	sheet, err := BuildMaterialSheet(task, task.Deadline)
	if err != nil {
		sheet = nil // 载荷坏了不挡详情；执行时会明确报错
	}
	utils.SuccessResp(c, "ok", gin.H{"task": task, "purchase_order": po, "material_sheet": sheet})
}

// Execute POST /api/purchase-tasks/:id/execute（手动触发自动执行器）
func (h *Handler) Execute(c *gin.Context) {
	ctx := c.Request.Context()
	task, err := h.repo.TaskByID(ctx, c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询采购任务失败", err)
		return
	}
	if task.ExecutorType != ExecutorAuto {
		utils.FailWithCode(c, utils.CodeValidate, "人工任务不能自动执行：走备料单 → 回填", nil, nil)
		return
	}
	if err := h.svc.EnqueueExecute(ctx, task.ID); err != nil {
		utils.ServerError(c, "投递执行任务失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "purchase_task.execute", Object: "purchase_task:" + task.ID})
	utils.SuccessResp(c, "已投递执行", nil)
}

// ConvertManual POST /api/purchase-tasks/:id/convert-manual {"assignee":"","note":""}
func (h *Handler) ConvertManual(c *gin.Context) {
	var req struct {
		Assignee string `json:"assignee"`
		Note     string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	task, err := h.svc.ConvertManual(c.Request.Context(), c.Param("id"), req.Assignee, req.Note)
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已转人工", task)
}

// MaterialSheet GET /api/purchase-tasks/:id/material-sheet
func (h *Handler) MaterialSheet(c *gin.Context) {
	sheet, err := h.svc.MaterialSheetFor(c.Request.Context(), c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "ok", sheet)
}

// MarkPaid POST /api/purchase-tasks/:id/mark-paid {"amount":"12.34","paid_at":"..."}
func (h *Handler) MarkPaid(c *gin.Context) {
	var req struct {
		Amount decimal.Decimal `json:"amount" binding:"required"`
		PaidAt *time.Time      `json:"paid_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	task, err := h.svc.MarkPaid(c.Request.Context(), c.Param("id"), req.Amount, req.PaidAt)
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已记已付款", task)
}

// FillBack POST /api/purchase-tasks/:id/fill-back（人工执行器回填）
func (h *Handler) FillBack(c *gin.Context) {
	var req FillBackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	task, err := h.svc.FillBack(c.Request.Context(), c.Param("id"), req)
	if errors.Is(err, ErrTaskNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "采购任务不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeValidate, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已回填", task)
}

// EnqueueExecute 投一条执行任务（操作台手动触发用）。
func (s *Service) EnqueueExecute(ctx context.Context, taskID string) error {
	if s.q == nil {
		return errors.New("队列未装配")
	}
	return s.q.Enqueue(ctx, ExecuteTask(taskID))
}

func queryInt(c *gin.Context, key string) int {
	n, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return 0
	}
	return n
}
