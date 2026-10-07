// 货源商品与按店映射的 HTTP 接口（总纲 §8 操作台「映射与报价」页）。
package catalog

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/utils"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Handler /api/supplier-offers、/api/offer-links。
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
	rg.GET("/supplier-offers", h.ListOffers)
	rg.POST("/supplier-offers", h.CreateOffer)
	rg.GET("/supplier-offers/:id", h.GetOffer)
	rg.PUT("/supplier-offers/:id", h.UpdateOffer)
	rg.DELETE("/supplier-offers/:id", h.DeleteOffer)

	rg.GET("/offer-links", h.ListLinks)
	rg.POST("/offer-links", h.CreateLink)
	rg.PUT("/offer-links/:id", h.UpdateLink)
	rg.DELETE("/offer-links/:id", h.DeleteLink)
}

// ---- 货源商品 ----

type offerReq struct {
	Platform            string           `json:"platform" binding:"required,oneof=1688 pdd taobao"`
	ItemID              string           `json:"item_id" binding:"required"`
	SkuID               string           `json:"sku_id"`
	URL                 string           `json:"url"`
	PurchasePrice       decimal.Decimal  `json:"purchase_price"`
	DomesticFreight     decimal.Decimal  `json:"domestic_freight"`
	Currency            string           `json:"currency"`
	Stock               int              `json:"stock"`
	PriceAlertThreshold *decimal.Decimal `json:"price_alert_threshold"`
	OrderChannel        string           `json:"order_channel" binding:"omitempty,oneof=self_use cross_border manual"`
	Followed            bool             `json:"followed"`
	Status              string           `json:"status" binding:"omitempty,oneof=active out_of_stock invalid"`
}

// ListOffers GET /api/supplier-offers?platform=&status=&keyword=
func (h *Handler) ListOffers(c *gin.Context) {
	rows, err := h.repo.ListOffers(c.Request.Context(), OfferFilter{
		Platform: c.Query("platform"),
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		utils.ServerError(c, "查询货源商品失败", err)
		return
	}
	utils.SuccessResp(c, "ok", rows)
}

// GetOffer GET /api/supplier-offers/:id
func (h *Handler) GetOffer(c *gin.Context) {
	o, err := h.repo.OfferByID(c.Request.Context(), c.Param("id"))
	if errors.Is(err, ErrOfferNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "货源商品不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询货源商品失败", err)
		return
	}
	utils.SuccessResp(c, "ok", o)
}

// CreateOffer POST /api/supplier-offers
func (h *Handler) CreateOffer(c *gin.Context) {
	var req offerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	o := &SupplierOffer{
		ID:                  snowflake.GenStringID(),
		Platform:            req.Platform,
		ItemID:              req.ItemID,
		SkuID:               req.SkuID,
		URL:                 req.URL,
		PurchasePrice:       req.PurchasePrice,
		DomesticFreight:     req.DomesticFreight,
		Currency:            req.Currency,
		Stock:               req.Stock,
		PriceAlertThreshold: req.PriceAlertThreshold,
		OrderChannel:        orDefault(req.OrderChannel, ChannelSelfUse),
		Followed:            req.Followed,
		Status:              req.Status,
	}
	if err := validateOffer(o); err != nil {
		failInvalid(c, err)
		return
	}
	if err := h.repo.CreateOffer(ctx, o); err != nil {
		utils.ServerError(c, "创建货源商品失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "supplier_offer.create", Object: "supplier_offer:" + o.ID, Detail: mustJSON(o)})
	utils.SuccessResp(c, "已创建", o)
}

// UpdateOffer PUT /api/supplier-offers/:id
func (h *Handler) UpdateOffer(c *gin.Context) {
	var req offerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	o, err := h.repo.OfferByID(ctx, c.Param("id"))
	if errors.Is(err, ErrOfferNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "货源商品不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询货源商品失败", err)
		return
	}
	before := mustJSON(o)
	o.Platform = req.Platform
	o.ItemID = req.ItemID
	o.SkuID = req.SkuID
	o.URL = req.URL
	o.PurchasePrice = req.PurchasePrice
	o.DomesticFreight = req.DomesticFreight
	o.Currency = orDefault(req.Currency, o.Currency)
	o.Stock = req.Stock
	o.PriceAlertThreshold = req.PriceAlertThreshold
	o.OrderChannel = orDefault(req.OrderChannel, o.OrderChannel)
	o.Followed = req.Followed
	o.Status = orDefault(req.Status, o.Status)
	if err := validateOffer(o); err != nil {
		failInvalid(c, err)
		return
	}
	if err := h.repo.UpdateOffer(ctx, o); err != nil {
		if errors.Is(err, ErrOfferNotFound) {
			utils.FailWithCode(c, utils.CodeNotFound, "货源商品不存在（可能刚被删除）", nil, nil)
			return
		}
		utils.ServerError(c, "更新货源商品失败", err)
		return
	}
	if fresh, err := h.repo.OfferByID(ctx, o.ID); err == nil {
		o = fresh
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "supplier_offer.update", Object: "supplier_offer:" + o.ID,
		Detail: diffJSON(before, mustJSON(o)),
	})
	utils.SuccessResp(c, "已更新", o)
}

// DeleteOffer DELETE /api/supplier-offers/:id
func (h *Handler) DeleteOffer(c *gin.Context) {
	ctx := c.Request.Context()
	o, err := h.repo.OfferByID(ctx, c.Param("id"))
	if errors.Is(err, ErrOfferNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "货源商品不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询货源商品失败", err)
		return
	}
	if err := h.repo.SoftDeleteOffer(ctx, o.ID); err != nil {
		utils.ServerError(c, "删除货源商品失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "supplier_offer.delete", Object: "supplier_offer:" + o.ID,
		Detail: mustJSON(map[string]string{"platform": o.Platform, "item_id": o.ItemID}),
	})
	utils.SuccessResp(c, "已删除", nil)
}

// ---- 按店映射 ----

type linkReq struct {
	StoreID         string `json:"store_id" binding:"required"`
	OzonOfferID     string `json:"ozon_offer_id" binding:"required"`
	SupplierOfferID string `json:"supplier_offer_id" binding:"required"`
	Priority        int    `json:"priority"`
	TargetStock     int    `json:"target_stock"`
}

// ListLinks GET /api/offer-links?store_id=&ozon_offer_id=&supplier_offer_id=
func (h *Handler) ListLinks(c *gin.Context) {
	rows, err := h.repo.ListLinks(c.Request.Context(), LinkFilter{
		StoreID:         c.Query("store_id"),
		OzonOfferID:     c.Query("ozon_offer_id"),
		SupplierOfferID: c.Query("supplier_offer_id"),
	})
	if err != nil {
		utils.ServerError(c, "查询映射失败", err)
		return
	}
	utils.SuccessResp(c, "ok", rows)
}

// CreateLink POST /api/offer-links
func (h *Handler) CreateLink(c *gin.Context) {
	var req linkReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	// 货源必须存在：挂一条映射指向空气，下单时才发现就晚了。
	if _, err := h.repo.OfferByID(ctx, req.SupplierOfferID); err != nil {
		if errors.Is(err, ErrOfferNotFound) {
			utils.FailWithCode(c, utils.CodeValidate, "supplier_offer_id 指向的货源商品不存在", nil, nil)
			return
		}
		utils.ServerError(c, "查询货源商品失败", err)
		return
	}
	l := &OfferLink{
		ID:              snowflake.GenStringID(),
		StoreID:         req.StoreID,
		OzonOfferID:     req.OzonOfferID,
		SupplierOfferID: req.SupplierOfferID,
		Priority:        maxInt(req.Priority, 1),
		TargetStock:     req.TargetStock,
	}
	if err := validateLink(l); err != nil {
		failInvalid(c, err)
		return
	}
	if err := h.repo.CreateLink(ctx, l); err != nil {
		if isDuplicate(err) {
			utils.FailWithCode(c, utils.CodeConflict, "该店铺该商品已挂过这条货源（改 priority 用 PUT）", nil, nil)
			return
		}
		utils.ServerError(c, "创建映射失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{Action: "offer_link.create", Object: "offer_link:" + l.ID, Detail: mustJSON(l)})
	utils.SuccessResp(c, "已创建", l)
}

// UpdateLink PUT /api/offer-links/:id（只改 priority / target_stock）
func (h *Handler) UpdateLink(c *gin.Context) {
	var req struct {
		Priority    int `json:"priority"`
		TargetStock int `json:"target_stock"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidateError(c, err)
		return
	}
	ctx := c.Request.Context()
	l, err := h.repo.LinkByID(ctx, c.Param("id"))
	if errors.Is(err, ErrLinkNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "映射不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询映射失败", err)
		return
	}
	before := mustJSON(l)
	l.Priority = maxInt(req.Priority, 1)
	l.TargetStock = req.TargetStock
	if err := validateLink(l); err != nil {
		failInvalid(c, err)
		return
	}
	if err := h.repo.UpdateLink(ctx, l); err != nil {
		if errors.Is(err, ErrLinkNotFound) {
			utils.FailWithCode(c, utils.CodeNotFound, "映射不存在（可能刚被删除）", nil, nil)
			return
		}
		utils.ServerError(c, "更新映射失败", err)
		return
	}
	if fresh, err := h.repo.LinkByID(ctx, l.ID); err == nil {
		l = fresh
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "offer_link.update", Object: "offer_link:" + l.ID,
		Detail: diffJSON(before, mustJSON(l)),
	})
	utils.SuccessResp(c, "已更新", l)
}

// DeleteLink DELETE /api/offer-links/:id
func (h *Handler) DeleteLink(c *gin.Context) {
	ctx := c.Request.Context()
	l, err := h.repo.LinkByID(ctx, c.Param("id"))
	if errors.Is(err, ErrLinkNotFound) {
		utils.FailWithCode(c, utils.CodeNotFound, "映射不存在", nil, nil)
		return
	}
	if err != nil {
		utils.ServerError(c, "查询映射失败", err)
		return
	}
	if err := h.repo.SoftDeleteLink(ctx, l.ID); err != nil {
		utils.ServerError(c, "删除映射失败", err)
		return
	}
	h.audit.Record(ctx, audit.Entry{
		Action: "offer_link.delete", Object: "offer_link:" + l.ID,
		Detail: mustJSON(map[string]string{"store_id": l.StoreID, "ozon_offer_id": l.OzonOfferID}),
	})
	utils.SuccessResp(c, "已删除", nil)
}

// ---- 小工具 ----

func failInvalid(c *gin.Context, err error) {
	var inv *ErrInvalid
	if errors.As(err, &inv) {
		utils.FailWithCode(c, utils.CodeValidate, inv.Reason, nil, nil)
		return
	}
	utils.FailWithCode(c, utils.CodeValidate, "参数校验失败", err, nil)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func maxInt(v, min int) int {
	if v < min {
		return min
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

func diffJSON(before, after string) string {
	b, _ := json.Marshal(map[string]string{"before": before, "after": after})
	return string(b)
}

// isDuplicate 唯一键冲突（db.Init 开了 TranslateError；兜底看错误文本——生成列唯一键
// 在某些驱动版本下翻译不全，store 包同款兜底）。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "Duplicate entry")
}
