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

// Validate 格式校验：平台单号、实付金额、国内快递号。
func (r *FillBackRequest) Validate() error {
	id := strings.TrimSpace(r.PlatformOrderID)
	if !platformOrderIDPattern.MatchString(id) {
		return fmt.Errorf("平台单号格式不对（6-64 位字母数字）")
	}
	if !r.Amount.GreaterThan(decimal.Zero) {
		return fmt.Errorf("实付金额必须大于 0")
	}
	if !trackingNoPattern.MatchString(strings.TrimSpace(r.DomesticTrackingNo)) {
		return fmt.Errorf("国内快递号格式不对（6-32 位字母数字或连字符）")
	}
	return nil
}

// FillBack 人工执行器回填：平台单号、实付、国内快递号 → 采购单入库 → 任务 shipped。
func (s *Service) FillBack(ctx context.Context, taskID string, req FillBackRequest) (*PurchaseTask, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	task, err := s.repo.TaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	switch task.Status {
	case StatusPending, StatusExecuting, StatusOrdered, StatusException:
	case StatusPaid, StatusShipped, StatusClosed:
		return nil, fmt.Errorf("任务已是「%s」，不能再回填", task.Status)
	}
	paidAt := req.PaidAt
	if paidAt == nil {
		now := s.now()
		paidAt = &now
	}
	po := &PurchaseOrder{
		TaskID:             task.ID,
		PlatformOrderID:    strings.TrimSpace(req.PlatformOrderID),
		Amount:             req.Amount,
		Currency:           currencyOr(payloadCurrency(task)),
		PaidAt:             paidAt,
		DomesticCarrier:    nullable(strings.TrimSpace(req.DomesticCarrier)),
		DomesticTrackingNo: nullable(strings.TrimSpace(req.DomesticTrackingNo)),
	}
	if err := s.repo.UpsertPurchaseOrder(ctx, po); err != nil {
		return nil, fmt.Errorf("记采购单失败: %w", err)
	}
	if err := s.repo.SetStatus(ctx, task.ID, StatusShipped); err != nil {
		return nil, err
	}
	s.syncOrderStatus(ctx, task.OrderID)
	s.audit.Record(ctx, audit.Entry{
		Action: "purchase_task.fill_back", Object: "purchase_task:" + task.ID,
		Detail: mustJSON(map[string]string{
			"platform_order_id": po.PlatformOrderID,
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
	}); err != nil {
		return nil, err
	}
	if err := s.repo.SetStatus(ctx, task.ID, StatusPaid); err != nil {
		return nil, err
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
