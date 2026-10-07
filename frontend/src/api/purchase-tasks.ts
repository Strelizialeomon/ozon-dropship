// 采购任务台（S1-D 的接口清单：/api/purchase-tasks —— 执行 / 转人工 / 备料单 / 回填）。
// ⚠️ 与 orders.ts 同理：D 未合并，按 spec-s1d §3 + 总纲 §5.1 / §6 / §7.3 手写，待对齐。
import { request } from './client';

export type TaskChannel = 'self_use' | 'cross_border' | 'manual';
export type TaskExecutor = 'auto' | 'manual';
export type TaskStatus = 'pending' | 'executing' | 'ordered' | 'paid' | 'shipped' | 'closed' | 'exception';

export interface PurchaseOrderDTO {
  id: string;
  platform_order_id: string | null;
  amount: string | null;
  currency: string | null;
  paid_at: string | null;
  domestic_carrier: string | null;
  domestic_tracking_no: string | null;
}

export interface PurchaseTaskDTO {
  id: string;
  order_id: string;
  posting_number: string;
  store_name: string;
  supplier_offer_id: string | null;
  channel: TaskChannel;
  executor_type: TaskExecutor;
  status: TaskStatus;
  assignee: string | null;
  deadline: string | null;
  purchase_order: PurchaseOrderDTO | null;
  created_at: string;
  updated_at: string;
}

export interface PurchaseTaskListQuery {
  page: number;
  page_size: number;
  status?: string;
  channel?: string;
  executor_type?: string;
  keyword?: string;
}

export interface PurchaseTaskListData {
  total: number;
  list: PurchaseTaskDTO[];
}

/** 备料单（人工渠道，总纲 §7.3）：收货地址 = 中转点、备注含 posting_number、不含买家个人信息。 */
export interface PrepSheetDTO {
  task_id: string;
  posting_number: string;
  platform: string; // 1688 / 拼多多 / 淘宝等（由货源平台决定）
  item_url: string;
  sku: string;
  qty: number;
  address: string;
  note: string;
  expected_days: number | null;
}

export interface MarkPaidReq {
  amount: string;
  platform_order_id?: string;
}

export interface BackfillReq {
  platform_order_id: string;
  amount: string;
  domestic_carrier?: string;
  domestic_tracking_no: string;
}

export function listPurchaseTasks(q: PurchaseTaskListQuery): Promise<PurchaseTaskListData> {
  return request<PurchaseTaskListData>({ method: 'GET', url: '/api/purchase-tasks', params: q });
}

/** 自动执行器立即下单（1688 预览 → 下单；S1 无免密支付，成功后停在 ordered）。 */
export function executePurchaseTask(id: string): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/execute` });
}

/** 转人工执行（新商家首单、通道不可用时的人工兜底）。 */
export function toManualPurchaseTask(id: string): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/to-manual` });
}

/** 记已付款（人工在 1688 付款后回填实付金额，任务 ordered → paid）。 */
export function markPaidPurchaseTask(id: string, body: MarkPaidReq): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/mark-paid`, data: body });
}

/** 回填平台单号 / 实付 / 国内快递号（总纲 §7.3；国内快递号不回传 Ozon）。 */
export function backfillPurchaseTask(id: string, body: BackfillReq): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/backfill`, data: body });
}

export function getPrepSheet(id: string): Promise<PrepSheetDTO> {
  return request<PrepSheetDTO>({ method: 'GET', url: `/api/purchase-tasks/${id}/prep-sheet` });
}
