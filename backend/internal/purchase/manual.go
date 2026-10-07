// 人工执行器（总纲 §7.3）：备料单生成、回填与格式校验。
// 人工渠道（拼多多 / 淘宝）没有下单 API，系统只备料 + 回填；任何形式的 RPA 都不做（§2.3）。
package purchase

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"

	"github.com/shopspring/decimal"
)

// 回填格式校验（子 spec §7.3：回填平台单号、实付、国内快递号，并校验格式）。
// 「抽检可达性」需要第三方快递查询服务（§13.2 未选型），S1 只做格式校验。
var (
	trackingNoPattern      = regexp.MustCompile(`^[A-Za-z0-9-]{6,32}$`)
	platformOrderIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{6,64}$`)
)

// MaterialSheet 备料单（总纲 §7.3）：商品链接、规格、数量、收货地址 = 中转点、
// 备注（含 posting_number）、期望时效——**不含买家个人信息**。
type MaterialSheet struct {
	TaskID        string         `json:"task_id"`
	PostingNumber string         `json:"posting_number"`
	Receiver      string         `json:"receiver"` // 中转点名称
	Address       string         `json:"address"`
	Contact       string         `json:"contact"`
	Deadline      *time.Time     `json:"deadline"`
	Note          string         `json:"note"`
	Items         []MaterialItem `json:"items"`
	Text          string         `json:"text"` // 人读版：复制给采购同事照着下单
}

// MaterialItem 备料单里的商品行。
type MaterialItem struct {
	ItemID   string          `json:"item_id"`
	SkuID    string          `json:"sku_id"`
	URL      string          `json:"url"`
	Qty      int             `json:"qty"`
	Price    decimal.Decimal `json:"price"`
	Currency string          `json:"currency"`
}

// BuildMaterialSheet 由任务构造备料单（纯函数，单测直接喂结构体）。
func BuildMaterialSheet(task *PurchaseTask, deadline *time.Time) (*MaterialSheet, error) {
	pay, err := task.DecodePayload()
	if err != nil {
		return nil, fmt.Errorf("任务载荷损坏: %w", err)
	}
	sheet := &MaterialSheet{
		TaskID:        task.ID,
		PostingNumber: pay.PostingNumber,
		Receiver:      pay.RelayName,
		Address:       pay.RelayAddress,
		Contact:       pay.RelayContact,
		Deadline:      deadline,
		Note:          pay.Note,
	}
	for _, it := range pay.Items {
		sheet.Items = append(sheet.Items, MaterialItem{
			ItemID: it.ItemID, SkuID: it.SkuID, URL: it.URL,
			Qty: it.Qty, Price: it.UnitPrice, Currency: it.Currency,
		})
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "备料单 %s（posting %s）\n", task.ID, pay.PostingNumber)
	fmt.Fprintf(&sb, "收货地址：%s %s（联系人：%s）\n", pay.RelayName, pay.RelayAddress, pay.RelayContact)
	if deadline != nil {
		fmt.Fprintf(&sb, "期望时效：%s 前送达收货地址\n", deadline.UTC().Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&sb, "备注请务必带上：%s\n", pay.PostingNumber)
	sb.WriteString("商品：\n")
	for _, it := range sheet.Items {
		fmt.Fprintf(&sb, "  - %s 规格:%s ×%d 参考价:%s %s 链接:%s\n",
			it.ItemID, it.SkuID, it.Qty, it.Price.String(), it.Currency, it.URL)
	}
	sheet.Text = sb.String()
	return sheet, nil
}

// FillBackRequest 人工回填（总纲 §7.3）。
type FillBackRequest struct {
	PlatformOrderID    string          `json:"platform_order_id" binding:"required"`
	Amount             decimal.Decimal `json:"amount" binding:"required"`
	DomesticCarrier    string          `json:"domestic_carrier"`
	DomesticTrackingNo string          `json:"domestic_tracking_no" binding:"required"`
	PaidAt             *time.Time      `json:"paid_at"`
}

// Validate 格式校验：国内快递号必填；平台单号 / 实付给了就校格式
// （「没给的话够不够用」由 FillBack 对着既有采购单判——自动任务已有单号与金额，
// 人工只是补快递号，不该被逼着重填一遍，重审 #2）。
func (r *FillBackRequest) Validate() error {
	if id := strings.TrimSpace(r.PlatformOrderID); id != "" && !platformOrderIDPattern.MatchString(id) {
		return fmt.Errorf("平台单号格式不对（6-64 位字母数字）")
	}
	if r.Amount.IsNegative() {
		return fmt.Errorf("实付金额不能为负")
	}
	if !trackingNoPattern.MatchString(strings.TrimSpace(r.DomesticTrackingNo)) {
		return fmt.Errorf("国内快递号格式不对（6-32 位字母数字或连字符）")
	}
	return nil
}

// FillBack 回填：平台单号 / 实付 / 国内快递号 → 采购单入库 → 任务 shipped。
//
// 允许的前置状态含 ordered / paid：「记已付款」之后卖家发货，人工把国内快递号补进来
// （子 spec §3.3「推进 paid，随后回填」；重审 #2）。平台单号与实付在既有采购单里
// 没有时才必填——自动任务下单时已记过。
func (s *Service) FillBack(ctx context.Context, taskID string, req FillBackRequest) (*PurchaseTask, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	task, err := s.repo.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	po, err := s.repo.PurchaseOrderByTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if (po == nil || po.PlatformOrderID == "") && strings.TrimSpace(req.PlatformOrderID) == "" {
		return nil, fmt.Errorf("这笔采购还没有平台单号，回填时请一并填上")
	}
	if (po == nil || po.Amount.IsZero()) && !req.Amount.GreaterThan(decimal.Zero) {
		return nil, fmt.Errorf("这笔采购还没有实付金额，回填时请一并填上")
	}

	paidAt := req.PaidAt
	if paided := po != nil && po.PaidAt != nil; paided {
		paidAt = po.PaidAt // 已经记过付款时间：不被回填抹掉
	}
	if paidAt == nil {
		now := s.now()
		paidAt = &now
	}
	row := &PurchaseOrder{
		TaskID:             task.ID,
		PlatformOrderID:    strings.TrimSpace(req.PlatformOrderID),
		Amount:             req.Amount,
		Currency:           currencyOr(payloadCurrency(task)),
		PaidAt:             paidAt,
		DomesticCarrier:    nullable(strings.TrimSpace(req.DomesticCarrier)),
		DomesticTrackingNo: nullable(strings.TrimSpace(req.DomesticTrackingNo)),
	}
	// 先写采购单（失败可重试、状态没动过），再 CAS 推进状态（并发下不倒退，重审 #7）。
	if err := s.repo.UpsertPurchaseOrder(ctx, row, true); err != nil {
		return nil, fmt.Errorf("记采购单失败: %w", err)
	}
	ok, err := s.repo.SetStatusFrom(ctx, task.ID,
		[]string{StatusPending, StatusExecuting, StatusOrdered, StatusException, StatusPaid}, StatusShipped)
	if err != nil {
		return nil, err
	}
	if !ok {
		fresh, _ := s.repo.TaskByID(ctx, task.ID)
		cur := task.Status
		if fresh != nil {
			cur = fresh.Status
		}
		return nil, fmt.Errorf("任务已是「%s」，不能再回填", cur)
	}
	s.syncOrderStatus(ctx, task.OrderID)
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.fill_back", Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]string{
			"platform_order_id": row.PlatformOrderID,
			"amount":            req.Amount.String(),
			"domestic_tracking": req.DomesticTrackingNo,
		}),
	})
	return s.repo.TaskByID(ctx, task.ID)
}

// MarkPaid 记已付款（自动任务的付款推进，总纲 §9·S1：人工在 1688 付款后回任务台登记）。
// GetOrder 核对为辅：核对失败不阻断（人工登记为主）。
func (s *Service) MarkPaid(ctx context.Context, taskID string, amount decimal.Decimal, paidAt *time.Time) (*PurchaseTask, error) {
	task, err := s.repo.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != StatusOrdered && task.Status != StatusException {
		return nil, fmt.Errorf("任务当前是「%s」，只有已下单（ordered）的任务才能记已付款", task.Status)
	}
	if !amount.GreaterThan(decimal.Zero) {
		return nil, fmt.Errorf("实付金额必须大于 0")
	}
	po, err := s.repo.PurchaseOrderByTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if po == nil {
		return nil, fmt.Errorf("任务还没有采购单，先执行 / 回填")
	}
	verifyNote := ""
	if buyer, berr := s.buyer(ctx); berr == nil && po.PlatformOrderID != "" {
		if bo, gerr := buyer.GetOrder(ctx, po.PlatformOrderID); gerr == nil && bo != nil {
			if !bo.Amount.IsZero() && !bo.Amount.Equal(amount) {
				verifyNote = fmt.Sprintf("GetOrder 核对：平台金额 %s，登记 %s", bo.Amount.String(), amount.String())
			}
			if bo.PaidAt != nil && paidAt == nil {
				paidAt = bo.PaidAt
			}
		}
	}
	if paidAt == nil {
		now := s.now()
		paidAt = &now
	}
	if err := s.repo.UpsertPurchaseOrder(ctx, &PurchaseOrder{
		TaskID: task.ID, Amount: amount, PaidAt: paidAt,
	}, true); err != nil {
		return nil, err
	}
	// CAS：已被回填推到 shipped / 已关单的任务不会被记已付款打回去（重审 #7）。
	ok, err := s.repo.SetStatusFrom(ctx, task.ID, []string{StatusOrdered, StatusException}, StatusPaid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("任务状态已变化，记已付款未生效，请刷新后重试")
	}
	s.syncOrderStatus(ctx, task.OrderID)
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.mark_paid", Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]string{"amount": amount.String(), "verify": verifyNote}),
	})
	return s.repo.TaskByID(ctx, task.ID)
}

// MaterialSheetFor 取任务的备料单。
func (s *Service) MaterialSheetFor(ctx context.Context, taskID string) (*MaterialSheet, error) {
	task, err := s.repo.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return BuildMaterialSheet(task, task.Deadline)
}

func payloadCurrency(task *PurchaseTask) string {
	pay, err := task.DecodePayload()
	if err != nil {
		return ""
	}
	return pay.Currency
}
