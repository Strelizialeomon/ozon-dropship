// 打包交接接口（总纲 §8：导出对照表 / 签收 / 备货 / 面单 / 传单号）+ 中转点增删改查。
package shipment

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"

	"github.com/gin-gonic/gin"
)

// Handler /api/shipments、/api/relay-points。
type Handler struct {
	svc    *Service
	relays *order.RelayRepo
	audit  *audit.Recorder
}

// NewHandler 构造。
func NewHandler(svc *Service, relays *order.RelayRepo, rec *audit.Recorder) *Handler {
	return &Handler{svc: svc, relays: relays, audit: rec}
}

// Register 挂到已过登录校验的路由组。
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/shipments", h.List)
	rg.GET("/shipments/handover-export", h.HandoverExport)
	rg.POST("/shipments/:id/receive", h.Receive)
	rg.POST("/shipments/:id/ship", h.Ship)
	rg.POST("/shipments/:id/tracking", h.SetTracking)
	rg.GET("/shipments/:id/label", h.Label)

	rg.GET("/relay-points", h.ListRelays)
	rg.POST("/relay-points", h.CreateRelay)
	rg.GET("/relay-points/:id", h.GetRelay)
	rg.PUT("/relay-points/:id", h.UpdateRelay)
	rg.DELETE("/relay-points/:id", h.DeleteRelay)
}

// List GET /api/shipments?store_id=&relay_point_id=&status=&page=&page_size=
func (h *Handler) List(c *gin.Context) {
	items, total, err := h.svc.List(c.Request.Context(), ListFilter{
		StoreID:      c.Query("store_id"),
		RelayPointID: c.Query("relay_point_id"),
		Statuses:     splitCSV(c.Query("status")),
		Page:         queryInt(c, "page"),
		PageSize:     queryInt(c, "page_size"),
	})
	if err != nil {
		utils.ServerError(c, "查询打包交接列表失败", err)
		return
	}
	utils.SuccessResp(c, "ok", gin.H{"total": total, "items": items})
}

// HandoverExport GET /api/shipments/handover-export?relay_point_id=
func (h *Handler) HandoverExport(c *gin.Context) {
	relayPointID := c.Query("relay_point_id")
	if strings.TrimSpace(relayPointID) == "" {
		utils.FailWithCode(c, utils.CodeValidate, "relay_point_id 不能为空", nil, nil)
		return
	}
	data, err := h.svc.HandoverCSV(c.Request.Context(), relayPointID)
	if err != nil {
		utils.ServerError(c, "导出交接对照表失败", err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="handover-`+relayPointID+`.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

// Receive POST /api/shipments/:id/receive（:id = 订单 ID；签收 → at_relay）
func (h *Handler) Receive(c *gin.Context) {
	row, err := h.svc.Receive(c.Request.Context(), c.Param("id"))
	if errors.Is(err, order.ErrOrderNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "订单不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已签收", row)
}

// Ship POST /api/shipments/:id/ship {"tracking_no":"","carrier":""}
func (h *Handler) Ship(c *gin.Context) {
	var req struct {
		TrackingNo string `json:"tracking_no"`
		Carrier    string `json:"carrier"`
	}
	_ = c.ShouldBindJSON(&req)
	res, err := h.svc.Ship(c.Request.Context(), c.Param("id"), req.TrackingNo, req.Carrier)
	if errors.Is(err, order.ErrOrderNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "订单不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已备货", res)
}

// SetTracking POST /api/shipments/:id/tracking {"tracking_no":"","carrier":""}
func (h *Handler) SetTracking(c *gin.Context) {
	var req struct {
		TrackingNo string `json:"tracking_no" binding:"required"`
		Carrier    string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	row, err := h.svc.SetTracking(c.Request.Context(), c.Param("id"), req.TrackingNo, req.Carrier)
	if errors.Is(err, order.ErrOrderNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "订单不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	utils.SuccessResp(c, "已传单号", row)
}

// Label GET /api/shipments/:id/label（面单 PDF，按需从 Ozon 拉）
func (h *Handler) Label(c *gin.Context) {
	pdf, filename, err := h.svc.Label(c.Request.Context(), c.Param("id"))
	if errors.Is(err, order.ErrOrderNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "订单不存在", nil, nil)
		return
	}
	if err != nil {
		utils.FailWithCode(c, utils.CodeFail, err.Error(), nil, nil)
		return
	}
	c.Header("Content-Disposition", `inline; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/pdf", pdf)
}

// ---- 中转点 ----

type relayReq struct {
	Name    string `json:"name" binding:"required"`
	Kind    string `json:"kind" binding:"required,oneof=forwarder own_warehouse"`
	Address string `json:"address"`
	Contact string `json:"contact"`
	Status  string `json:"status" binding:"omitempty,oneof=active disabled"`
}

// ListRelays GET /api/relay-points
func (h *Handler) ListRelays(c *gin.Context) {
	rows, err := h.relays.List(c.Request.Context())
	if err != nil {
		utils.ServerError(c, "查询中转点失败", err)
		return
	}
	utils.SuccessResp(c, "ok", rows)
}

// GetRelay GET /api/relay-points/:id
func (h *Handler) GetRelay(c *gin.Context) {
	p, err := h.relays.ByID(c.Request.Context(), c.Param("id"))
	if errors.Is(err, order.ErrRelayPointNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "中转点不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询中转点失败", err)
		return
	}
	utils.SuccessResp(c, "ok", p)
}

// CreateRelay POST /api/relay-points
func (h *Handler) CreateRelay(c *gin.Context) {
	var req relayReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	p := &order.RelayPoint{
		ID:      snowflake.GenStringID(),
		Name:    req.Name,
		Kind:    req.Kind,
		Address: req.Address,
		Contact: req.Contact,
		Status:  orDefault(req.Status, order.RelayStatusActive),
	}
	if err := h.relays.Create(ctx, p); err != nil {
		if isDup(err) {
			utils.FailWithCode(c, utils.CodeConflict, "中转点名已存在", nil, nil)
			return
		}
		utils.ServerError(c, "创建中转点失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "relay_point.create", Object: "relay_point:" + p.ID, Detail: mustJSON(p)})
	utils.SuccessResp(c, "已创建", p)
}

// UpdateRelay PUT /api/relay-points/:id
func (h *Handler) UpdateRelay(c *gin.Context) {
	var req relayReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	p, err := h.relays.ByID(ctx, c.Param("id"))
	if errors.Is(err, order.ErrRelayPointNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "中转点不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询中转点失败", err)
		return
	}
	before := mustJSON(p)
	p.Name = req.Name
	p.Kind = req.Kind
	p.Address = req.Address
	p.Contact = req.Contact
	p.Status = orDefault(req.Status, p.Status)
	if err := h.relays.Update(ctx, p); err != nil {
		if errors.Is(err, order.ErrRelayPointNotFound) {
			utils.FailWithCode(c, utils.CodeNotFound, "中转点不存在（可能刚被删除）", nil, nil)
			return
		}
		if isDup(err) {
			utils.FailWithCode(c, utils.CodeConflict, "中转点名已存在", nil, nil)
			return
		}
		utils.ServerError(c, "更新中转点失败", err)
		return
	}
	if fresh, err := h.relays.ByID(ctx, p.ID); err == nil {
		p = fresh
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "relay_point.update", Object: "relay_point:" + p.ID,
		Detail: mustJSON(map[string]string{"before": before, "after": mustJSON(p)}),
	})
	utils.SuccessResp(c, "已更新", p)
}

// DeleteRelay DELETE /api/relay-points/:id
func (h *Handler) DeleteRelay(c *gin.Context) {
	ctx := c.Request.Context()
	p, err := h.relays.ByID(ctx, c.Param("id"))
	if errors.Is(err, order.ErrRelayPointNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "中转点不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询中转点失败", err)
		return
	}
	if err := h.relays.SoftDelete(ctx, p.ID); err != nil {
		utils.ServerError(c, "删除中转点失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "relay_point.delete", Object: "relay_point:" + p.ID,
		Detail: mustJSON(map[string]string{"name": p.Name}),
	})
	utils.SuccessResp(c, "已删除", nil)
}

// ---- 小工具 ----

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func isDup(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Duplicate entry")
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

func queryInt(c *gin.Context, key string) int {
	n, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return 0
	}
	return n
}
