// 订单接口（总纲 §8 操作台「订单工作台」：列表 / 筛选 / 详情 / 批量）。
package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
)

// Handler /api/orders。
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
	rg.GET("/orders", h.List)
	rg.GET("/orders/:id", h.Get)
	rg.POST("/orders/batch", h.Batch)
}

// List GET /api/orders?store_id=&status=new,purchasing&keyword=&page=&page_size=
func (h *Handler) List(c *gin.Context) {
	from, err := parseTime(c.Query("from"))
	if err != nil {
		utils.FailWithCode(c, utils.CodeValidate, "from 时间格式不对（RFC3339）", nil, nil)
		return
	}
	to, err := parseTime(c.Query("to"))
	if err != nil {
		utils.FailWithCode(c, utils.CodeValidate, "to 时间格式不对（RFC3339）", nil, nil)
		return
	}
	rows, total, err := h.repo.List(c.Request.Context(), OrderFilter{
		StoreID:    c.Query("store_id"),
		Statuses:   splitCSV(c.Query("status")),
		OzonStatus: c.Query("ozon_status"),
		Tpl:        c.Query("tpl_integration_type"),
		Keyword:    c.Query("keyword"),
		From:       from,
		To:         to,
		Page:       parseInt(c.Query("page")),
		PageSize:   parseInt(c.Query("page_size")),
	})
	if err != nil {
		utils.ServerError(c, "查询订单失败", err)
		return
	}
	utils.SuccessResp(c, "ok", gin.H{"total": total, "items": rows})
}

// Get GET /api/orders/:id（含商品行）
func (h *Handler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	o, err := h.repo.ByID(ctx, c.Param("id"))
	if errors.Is(err, ErrOrderNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "订单不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询订单失败", err)
		return
	}
	items, err := h.repo.Items(ctx, o.ID)
	if err != nil {
		utils.ServerError(c, "查询订单行失败", err)
		return
	}
	utils.SuccessResp(c, "ok", gin.H{"order": o, "items": items})
}

type batchReq struct {
	IDs          []string `json:"ids" binding:"required,min=1"`
	Action       string   `json:"action" binding:"required,oneof=plan_purchase set_relay_point"`
	RelayPointID string   `json:"relay_point_id"`
}

// Batch POST /api/orders/batch —— 操作台批量动作（每条独立成败，不整批回滚）。
func (h *Handler) Batch(c *gin.Context) {
	var req batchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	if req.Action == "set_relay_point" && strings.TrimSpace(req.RelayPointID) == "" {
		utils.FailWithCode(c, utils.CodeValidate, "set_relay_point 需要 relay_point_id", nil, nil)
		return
	}
	ctx := c.Request.Context()

	type failure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	failed := make([]failure, 0)
	succeeded := 0
	for _, id := range req.IDs {
		var err error
		switch req.Action {
		case "plan_purchase":
			// 幂等：任务键 = 订单号，重复投递自动去重。
			err = h.svc.EnqueuePlan(ctx, id)
		case "set_relay_point":
			_, err = h.repo.SetRelayPoint(ctx, id, req.RelayPointID)
		}
		if err != nil {
			failed = append(failed, failure{ID: id, Error: err.Error()})
			continue
		}
		succeeded++
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "order.batch." + req.Action,
		Object: fmt.Sprintf("orders:%d", len(req.IDs)),
		Detail: mustJSON(map[string]any{
			"ids": req.IDs, "relay_point_id": req.RelayPointID,
			"succeeded": succeeded, "failed": failed,
		}),
	})
	utils.SuccessResp(c, "已处理", gin.H{"succeeded": succeeded, "failed": failed})
}

// ---- 小工具 ----

// EnqueuePlan 投一条采购计划任务（批量动作与轮询共用；订单不存在时明确报错）。
func (s *Service) EnqueuePlan(ctx context.Context, orderID string) error {
	if _, err := s.repo.ByID(ctx, orderID); err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return fmt.Errorf("订单 %s 不存在", orderID)
		}
		return err
	}
	if s.q == nil {
		return errors.New("队列未装配")
	}
	return s.q.Enqueue(ctx, PlanTask(orderID))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parseTime(s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
