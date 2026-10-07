// S1-D 装配：把 internal/ozon 与 internal/alibaba 适配成 S1-D 的「使用方接口」。
//
// D 的三个小接口都定义在使用方（ADR-20261007-go-package-deps-v2）：
//   - order.PostingSource（拉单：ListPostings / ListUnfulfilled / GetPosting）
//   - shipment.PostingFulfiller（履约：ShipPosting / GetPosting / GetPackageLabel / SetTrackingNumber）
//   - purchase.BuyerClient（1688 买家侧五件套）
//
// 适配放组合根（与 S1-B 的 cmd/api/ozon.go 同一套路）：实现方（ozon / alibaba）不 import 业务包，
// 业务包也不 import 实现方——只有本文件同时认识两边。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/alibaba"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/config"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/ratelimit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/order"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/ozon"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/purchase"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/shipment"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/store"

	"github.com/shopspring/decimal"
)

// ---- 通用：按店铺 + 凭据造 Ozon 客户端 ----

// newOzonClient 造一家店的 Ozon 客户端（限流桶按 Client-Id，官方口径每 Client-Id 50 次/秒）。
func newOzonClient(limiter *ratelimit.Registry, shop store.Shop, cred *store.Decrypted) (*ozon.Client, error) {
	if shop.ClientID == "" {
		return nil, fmt.Errorf("店铺 %s 未配 Client-Id", shop.Name)
	}
	apiKey := ""
	if cred != nil {
		apiKey = cred.Payload["api_key"]
	}
	if apiKey == "" {
		return nil, fmt.Errorf("店铺 %s 的 Ozon 凭据缺 api_key 字段", shop.Name)
	}
	return ozon.New(limiter, ozon.Options{ClientID: shop.ClientID, APIKey: apiKey, Subject: shop.ClientID}), nil
}

// ---- Ozon → order.PostingSource（拉单）----

// ozonPostingSource 把 ozon.Client 适配成 order.PostingSource。
type ozonPostingSource struct{ c *ozon.Client }

// ListPostings 按时间窗拉单（翻页取全：D 的窗口语义是「这一窗里的全部」）。
func (s *ozonPostingSource) ListPostings(ctx context.Context, req order.ListPostingsRequest) ([]order.Posting, error) {
	var out []order.Posting
	cursor := ""
	for {
		page, err := s.c.ListPostings(ctx, ozon.ListPostingsParams{
			Since:    ozon.Time{Time: req.Since.UTC()},
			To:       ozon.Time{Time: req.To.UTC()},
			Statuses: req.Statuses,
			Cursor:   cursor,
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Postings {
			out = append(out, postingFromList(&page.Postings[i]))
		}
		if !page.HasNext || page.Cursor == "" {
			return out, nil
		}
		cursor = page.Cursor
	}
}

// ListUnfulfilled 拉未完成单（老单状态变化的兜底）。
func (s *ozonPostingSource) ListUnfulfilled(ctx context.Context, req order.ListPostingsRequest) ([]order.Posting, error) {
	var out []order.Posting
	cursor := ""
	for {
		page, err := s.c.ListUnfulfilled(ctx, ozon.ListUnfulfilledParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for i := range page.Postings {
			out = append(out, postingFromList(&page.Postings[i]))
		}
		if !page.HasNext || page.Cursor == "" {
			return out, nil
		}
		cursor = page.Cursor
	}
}

// GetPosting 单详情（新单 / 缺商品行的老单拉权威数据）。
func (s *ozonPostingSource) GetPosting(ctx context.Context, postingNumber string) (*order.Posting, error) {
	d, err := s.c.GetPosting(ctx, postingNumber)
	if err != nil {
		return nil, err
	}
	p := postingFromDetail(d)
	return &p, nil
}

// ozonPostingSourceFactory 装配层注入 order 的工厂。
func ozonPostingSourceFactory(limiter *ratelimit.Registry) order.PostingSourceFactory {
	return func(shop store.Shop, cred *store.Decrypted) (order.PostingSource, error) {
		c, err := newOzonClient(limiter, shop, cred)
		if err != nil {
			return nil, err
		}
		return &ozonPostingSource{c: c}, nil
	}
}

// ---- 字段映射：ozon → order ----

func postingFromList(p *ozon.Posting) order.Posting {
	out := order.Posting{
		PostingNumber:       p.PostingNumber,
		OrderNumber:         p.OrderNumber,
		ParentPostingNumber: p.ParentPostingNumber,
		Status:              p.Status,
		Substatus:           p.Substatus,
		TplIntegrationType:  p.TplIntegrationType,
	}
	if !p.ShipmentDate.IsZero() {
		t := p.ShipmentDate.UTC()
		out.ShipDeadline = &t
	}
	for _, prod := range p.Products {
		it := order.PostingItem{OzonOfferID: prod.OfferID, Qty: int(prod.Quantity)}
		if prod.Price != nil {
			it.Price = parseDecimal(prod.Price.Amount)
			it.Currency = prod.Price.Currency
		}
		out.Currency = firstNonEmpty(out.Currency, it.Currency)
		out.Items = append(out.Items, it)
	}
	return out
}

func postingFromDetail(p *ozon.PostingDetail) order.Posting {
	out := order.Posting{
		PostingNumber:       p.PostingNumber,
		OrderNumber:         p.OrderNumber,
		ParentPostingNumber: p.ParentPostingNumber,
		Status:              p.Status,
		Substatus:           p.Substatus,
		TplIntegrationType:  p.TplIntegrationType,
	}
	if !p.ShipmentDate.IsZero() {
		t := p.ShipmentDate.UTC()
		out.ShipDeadline = &t
	}
	for _, prod := range p.Products {
		it := order.PostingItem{
			OzonOfferID: prod.OfferID,
			Qty:         int(prod.Quantity),
			Price:       parseDecimal(prod.Price),
			Currency:    prod.CurrencyCode,
		}
		out.Currency = firstNonEmpty(out.Currency, it.Currency)
		out.Items = append(out.Items, it)
	}
	return out
}

// parseDecimal 解析金额字符串；解析不了按 0（金额不进判定，宁可为 0 也不报错中断拉单）。
func parseDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decimal.Zero
	}
	return d
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- Ozon → shipment.PostingFulfiller（备货 / 面单 / 传单号）----

// ozonFulfiller 把 ozon.Client 适配成 shipment.PostingFulfiller。
type ozonFulfiller struct {
	c *ozon.Client
	// labelTimeout / labelInterval 面单两步流程的轮询预算（面单生成要几十秒）。
	labelTimeout  time.Duration
	labelInterval time.Duration
	// download 取面单文件（默认 http.Get + 大小上限）；测试可替换。
	download func(ctx context.Context, url string) ([]byte, error)
}

// ShipPosting 备货：先读 posting 详情拿商品行，再按「单包裹」组装。
// （S1 单包裹；request 里的 packages 数组是拆多包裹用的，D 的接口不暴露。）
func (f *ozonFulfiller) ShipPosting(ctx context.Context, postingNumber string) error {
	detail, err := f.c.GetPosting(ctx, postingNumber)
	if err != nil {
		return fmt.Errorf("备货前读 posting 详情失败: %w", err)
	}
	if len(detail.Products) == 0 {
		return fmt.Errorf("posting %s 没有商品行，无法组装", postingNumber)
	}
	pkg := ozon.ShipPackage{Products: make([]ozon.ShipProduct, 0, len(detail.Products))}
	for _, prod := range detail.Products {
		if prod.SKU <= 0 || prod.Quantity <= 0 {
			return fmt.Errorf("posting %s 商品行缺 sku/数量（sku=%d qty=%d）", postingNumber, prod.SKU, prod.Quantity)
		}
		pkg.Products = append(pkg.Products, ozon.ShipProduct{ProductID: prod.SKU, Quantity: prod.Quantity})
	}
	if _, err := f.c.ShipPosting(ctx, postingNumber, []ozon.ShipPackage{pkg}); err != nil {
		return err
	}
	return nil
}

// GetPosting 查 posting 状态（shipment 复核 substatus 用）。
func (f *ozonFulfiller) GetPosting(ctx context.Context, postingNumber string) (*shipment.PostingState, error) {
	d, err := f.c.GetPosting(ctx, postingNumber)
	if err != nil {
		return nil, err
	}
	return &shipment.PostingState{Status: d.Status, Substatus: d.Substatus}, nil
}

// GetPackageLabel 取面单 PDF：走两步流程（旧同步接口 2026-11-02 关停）——
// create 拿 task_id → 轮询 get 直到 completed → 从 file_url 下载。
func (f *ozonFulfiller) GetPackageLabel(ctx context.Context, postingNumber string) ([]byte, error) {
	tasks, err := f.c.CreatePackageLabel(ctx, []string{postingNumber})
	if err != nil {
		return nil, fmt.Errorf("创建面单任务失败: %w", err)
	}
	if len(tasks) == 0 {
		return nil, errors.New("创建面单任务没有返回 task_id")
	}
	taskID := tasks[0].TaskID

	deadline := time.Now().Add(f.labelTimeout)
	for {
		res, err := f.c.GetPackageLabelTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("查面单任务失败: %w", err)
		}
		if res.Status != nil {
			switch res.Status.Code {
			case ozon.LabelTaskCompleted:
				if res.FileURL == "" {
					return nil, errors.New("面单任务已完成但没有 file_url")
				}
				return f.download(ctx, res.FileURL)
			case ozon.LabelTaskError:
				msg := ""
				if res.Error != nil {
					msg = res.Error.Code + ": " + res.Error.Message
				}
				return nil, fmt.Errorf("面单任务出错: %s", msg)
			}
		}
		if time.Now().After(deadline) {
			// 不算失败：面单要几十秒才生成好，让操作台过会儿再点一次。
			return nil, fmt.Errorf("面单还在生成中（已等 %s），稍后再试", f.labelTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.labelInterval):
		}
	}
}

// SetTrackingNumber 传单号（接口只收 posting + 单号，承运商不进请求体）。
func (f *ozonFulfiller) SetTrackingNumber(ctx context.Context, postingNumber, trackingNo, _ string) error {
	results, err := f.c.SetTrackingNumber(ctx, []ozon.TrackingNumber{
		{PostingNumber: postingNumber, TrackingNumber: trackingNo},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.PostingNumber == postingNumber && !r.OK {
			return fmt.Errorf("平台拒绝该单号: %s", r.Error)
		}
	}
	return nil
}

// ozonFulfillerFactory 装配层注入 shipment 的工厂。
func ozonFulfillerFactory(limiter *ratelimit.Registry) shipment.FulfillerFactory {
	return func(shop store.Shop, cred *store.Decrypted) (shipment.PostingFulfiller, error) {
		c, err := newOzonClient(limiter, shop, cred)
		if err != nil {
			return nil, err
		}
		return &ozonFulfiller{
			c:             c,
			labelTimeout:  30 * time.Second,
			labelInterval: 3 * time.Second,
			download:      downloadURL,
		}, nil
	}
}

// maxLabelBytes 面单 PDF 大小上限（防给个超大地址把内存打满）。
const maxLabelBytes = 16 << 20

// downloadURL 下载面单文件（file_url 是 Ozon 给的临时地址）。
func downloadURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载面单失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载面单失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLabelBytes))
	if err != nil {
		return nil, fmt.Errorf("读面单内容失败: %w", err)
	}
	if len(body) == 0 {
		return nil, errors.New("面单文件是空的")
	}
	return body, nil
}

// ---- 1688 → purchase.BuyerClient ----

// alibabaBuyer 把 alibaba.Client 适配成 purchase.BuyerClient。
type alibabaBuyer struct{ c *alibaba.Client }

// PreviewOrder 下单预览：1688 的金额单位是「分」，换算成元。
func (b *alibabaBuyer) PreviewOrder(ctx context.Context, req purchase.PreviewRequest) (*purchase.PreviewResult, error) {
	offerID, err := strconv.ParseUint(strings.TrimSpace(req.ItemID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("item_id %q 不是 1688 商品 id: %w", req.ItemID, err)
	}
	res, err := b.c.PreviewOrder(ctx, alibaba.PreviewOrderRequest{
		Address: toAlibabaAddress(req.Address),
		CargoList: []alibaba.Cargo{{
			OfferID:  offerID,
			SpecID:   req.SkuID,
			Quantity: float64(req.Qty),
		}},
	})
	if err != nil {
		return nil, err
	}
	out := &purchase.PreviewResult{CanOrder: true}
	if len(res.OrderPreviews) == 0 {
		out.CanOrder = false
		out.Reason = purchase.PreviewReasonOther
		out.Message = "预览没有返回结果"
		return out, nil
	}
	pv := res.OrderPreviews[0]
	out.Freight = centsToYuan(pv.SumCarriage)
	if !pv.Status {
		out.CanOrder = false
		out.Reason = previewReason(pv.ResultCode, pv.Message)
		out.Message = firstNonEmpty(pv.Message, pv.ResultCode)
	}
	return out, nil
}

// previewReason 把 1688 的失败原因归一到 D 的三个分支。
func previewReason(code, message string) string {
	text := strings.ToLower(code + " " + message)
	switch {
	case strings.Contains(text, "address") || strings.Contains(text, "地址"):
		return purchase.PreviewReasonAddressInvalid
	case strings.Contains(text, "new") || strings.Contains(text, "新商家") ||
		strings.Contains(text, "老商家") || strings.Contains(text, "first") ||
		strings.Contains(text, "首次"):
		return purchase.PreviewReasonNewSeller
	default:
		return purchase.PreviewReasonOther
	}
}

// CreateOrder 下单：收货地址 = 中转点，留言与外部单号都带 posting#任务标记。
func (b *alibabaBuyer) CreateOrder(ctx context.Context, req purchase.CreateOrderRequest) (*purchase.BuyerOrder, error) {
	offerID, err := strconv.ParseUint(strings.TrimSpace(req.ItemID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("item_id %q 不是 1688 商品 id: %w", req.ItemID, err)
	}
	res, err := b.c.CreateOrder(ctx, alibaba.CreateOrderRequest{
		Address: toAlibabaAddress(req.Address),
		CargoList: []alibaba.Cargo{{
			OfferID:  offerID,
			SpecID:   req.SkuID,
			Quantity: float64(req.Qty),
		}},
		Message:    req.Remark,
		OutOrderID: req.OutOrderID,
	})
	if err != nil {
		return nil, err
	}
	orderID := string(res.OrderID)
	if orderID == "" {
		return nil, errors.New("下单返回里没有 orderId")
	}
	if len(res.FailedOfferList) > 0 {
		// 整体成功但个别商品失败：这是「部分下单」，必须让人看见，不能当成功吞掉。
		f := res.FailedOfferList[0]
		return nil, fmt.Errorf("部分商品下单失败（offer %v: %s），需人工核对", f.OfferID, f.ErrorMessage)
	}
	return &purchase.BuyerOrder{
		PlatformOrderID: orderID,
		OutOrderID:      req.OutOrderID,
		Amount:          centsToYuan(res.TotalSuccessAmount),
		Currency:        "CNY",
		Status:          "created",
		Remark:          req.Remark,
	}, nil
}

// GetOrder 订单详情（核对实付金额 / 付款时间）。
func (b *alibabaBuyer) GetOrder(ctx context.Context, platformOrderID string) (*purchase.BuyerOrder, error) {
	id, err := parseOrderID(platformOrderID)
	if err != nil {
		return nil, err
	}
	o, err := b.c.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	return buyerOrderFromAlibaba(o), nil
}

// ListBuyerOrders 按时间窗查买家订单（下单防重核对用）。
func (b *alibabaBuyer) ListBuyerOrders(ctx context.Context, req purchase.ListBuyerOrdersRequest) ([]purchase.BuyerOrder, error) {
	list, err := b.c.ListBuyerOrders(ctx, alibaba.ListBuyerOrdersRequest{
		CreateStartTime: req.Since.UTC(),
		CreateEndTime:   req.To.UTC(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]purchase.BuyerOrder, 0, len(list.Orders))
	for i := range list.Orders {
		out = append(out, *buyerOrderFromAlibaba(&list.Orders[i]))
	}
	return out, nil
}

// GetLogistics 买家视角物流（国内段单号与承运商）。
func (b *alibabaBuyer) GetLogistics(ctx context.Context, platformOrderID string) (*purchase.Logistics, error) {
	id, err := parseOrderID(platformOrderID)
	if err != nil {
		return nil, err
	}
	lg, err := b.c.GetLogistics(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &purchase.Logistics{}
	if len(lg.Infos) > 0 {
		out.Carrier = lg.Infos[0].LogisticsCompanyName
		out.TrackingNo = lg.Infos[0].LogisticsBillNo
	}
	if len(lg.Traces) > 0 && len(lg.Traces[0].Steps) > 0 {
		steps := lg.Traces[0].Steps
		last := steps[len(steps)-1]
		out.LatestEvent = last.Remark
		out.UpdatedAt = parseFlexibleTime(last.AcceptTime)
	}
	if out.TrackingNo == "" && len(lg.Traces) > 0 {
		out.TrackingNo = lg.Traces[0].LogisticsBillNo
	}
	if out.Carrier == "" && len(lg.Infos) > 0 {
		out.Carrier = lg.Infos[0].LogisticsCompanyName
	}
	return out, nil
}

// buyerOrderFromAlibaba 详情/列表 → D 的 BuyerOrder。
func buyerOrderFromAlibaba(o *alibaba.Order) *purchase.BuyerOrder {
	base := o.BaseInfo
	out := &purchase.BuyerOrder{
		PlatformOrderID: string(base.ID),
		OutOrderID:      base.OutOrderID,
		Amount:          base.TotalAmount,
		Currency:        "CNY",
		Status:          base.Status,
	}
	if out.PlatformOrderID == "" {
		out.PlatformOrderID = string(base.IDOfStr)
	}
	if t := base.PayTime.Time; !t.IsZero() {
		paid := t.UTC()
		out.PaidAt = &paid
	}
	if t := base.CreateTime.Time; !t.IsZero() {
		out.CreatedAt = t.UTC()
	}
	if o.NativeLogistics != nil && len(o.NativeLogistics.Items) > 0 {
		out.TrackingNo = o.NativeLogistics.Items[0].LogisticsBillNo
	}
	return out
}

// parseFlexibleTime 1688 轨迹时间是字符串（"2026-10-07 12:00:00" 一类），
// 解析不了就返回零值（轨迹时间不进判定）。
func parseFlexibleTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.UTC); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func parseOrderID(s string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("订单号 %q 不是 1688 订单 id: %w", s, err)
	}
	return id, nil
}

// centsToYuan 1688 预览/下单响应的金额单位是「分」（官方字段说明）。
func centsToYuan(cents int64) decimal.Decimal {
	return decimal.NewFromInt(cents).Div(decimal.NewFromInt(100))
}

// alibabaBuyerFactory 装配层注入 purchase 的工厂：1688 客户端全进程共用一个
// （内部有 token 缓存与限流桶），所以这里忽略 D 传进来的凭据、返回同一个实例。
func alibabaBuyerFactory(client *alibaba.Client) purchase.BuyerClientFactory {
	return func(purchase.BuyerCredentials) (purchase.BuyerClient, error) {
		if client == nil {
			return nil, errors.New("1688 客户端未构造（检查 1688 凭据与限流配置）")
		}
		return &alibabaBuyer{c: client}, nil
	}
}

// ---- 装配辅助 ----

// newAlibabaClient 造 1688 客户端（企业级凭据走 S1-A 的凭据服务）。
func newAlibabaClient(cfg *config.AppConfig, limiter *ratelimit.Registry, creds *store.CredentialService) (*alibaba.Client, error) {
	return alibaba.NewClient(alibaba.Config{
		Registry:    limiter,
		Credentials: credentialStoreAdapter{svc: creds},
	})
}

// credentialStoreAdapter 把 store.CredentialService 适配成 alibaba.CredentialStore
// （接口放使用方：alibaba 不 import store）。
type credentialStoreAdapter struct{ svc *store.CredentialService }

func (a credentialStoreAdapter) Get(ctx context.Context, storeID, kind string) (map[string]string, *time.Time, error) {
	dec, err := a.svc.Get(ctx, storeID, kind)
	if err != nil {
		return nil, nil, err
	}
	return dec.Payload, dec.ExpiresAt, nil
}

func (a credentialStoreAdapter) Put(ctx context.Context, storeID, kind string, payload map[string]string, expiresAt *time.Time) error {
	_, err := a.svc.Put(ctx, storeID, kind, payload, expiresAt)
	return err
}

func (a credentialStoreAdapter) SetExpiry(ctx context.Context, storeID, kind string, expiresAt, lastVerifiedAt *time.Time) error {
	return a.svc.SetExpiry(ctx, storeID, kind, expiresAt, lastVerifiedAt)
}

// ---- 地址：中转点自由文本 → 1688 要的省 / 市 / 区结构 ----

// 地址拆分（best-effort）：1688 的 addressParam 要 provinceText / cityText / areaText 三段，
// 而中转点地址在库里是一行自由文本（relay_points.address）。这里按最常见的行政写法切：
//
//	「广东省深圳市宝安区航城大道 1 号 3 楼」→ 广东省 / 深圳市 / 宝安区 / 航城大道 1 号 3 楼
//	「义乌市北苑街道 88 号」            → 省空 / 义乌市 / 空 / 北苑街道 88 号
//
// 切不出来就把整串放进详细地址、省市区留空——1688 会按地址校验失败拒单，
// 落到 D 的 address_invalid 异常池（比瞎猜一个省市更安全）。
var (
	reProvince = regexp.MustCompile(`^(.{2,10}?(?:省|自治区|特别行政区))`)
	reCity     = regexp.MustCompile(`^(.{2,10}?(?:市|自治州|地区|盟))`)
	reArea     = regexp.MustCompile(`^(.{2,10}?(?:区|县|市|旗))`)
)

// municipality 直辖市：没有「省」字，省 = 市。
var municipality = map[string]bool{"北京": true, "上海": true, "天津": true, "重庆": true}

// splitChineseAddress 拆地址；返回 (省, 市, 区, 详细)。
func splitChineseAddress(addr string) (string, string, string, string) {
	rest := strings.TrimSpace(addr)
	var province, city, area string

	if loc := reProvince.FindStringIndex(rest); loc != nil {
		province = rest[loc[0]:loc[1]]
		rest = rest[loc[1]:]
	}
	if city == "" && province == "" {
		for name := range municipality {
			if strings.HasPrefix(rest, name) {
				province, city = name+"市", name+"市"
				rest = strings.TrimPrefix(rest, name+"市") // 「上海市浦东新区…」：市字也要吃掉
				break
			}
		}
	}
	if city == "" {
		if loc := reCity.FindStringIndex(rest); loc != nil {
			city = rest[loc[0]:loc[1]]
			rest = rest[loc[1]:]
		}
	}
	if area == "" {
		if loc := reArea.FindStringIndex(rest); loc != nil {
			area = rest[loc[0]:loc[1]]
			rest = rest[loc[1]:]
		}
	}
	return province, city, area, strings.TrimSpace(rest)
}

func toAlibabaAddress(a purchase.Address) alibaba.Address {
	province, city, area, detail := splitChineseAddress(a.Address)
	if detail == "" {
		detail = strings.TrimSpace(a.Address)
	}
	return alibaba.Address{
		FullName:     a.Name,
		Mobile:       a.Phone,
		Phone:        "",
		ProvinceText: province,
		CityText:     city,
		AreaText:     area,
		Address:      detail,
	}
}
