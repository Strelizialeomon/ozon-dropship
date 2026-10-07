// 采购任务台接口（后端已实现：backend/internal/purchase/handler.go、model.go、manual.go）。
// 字段逐个照 Go struct 的 json tag；路径用后端实际注册的（convert-manual / material-sheet / fill-back）。
import { request } from './client';

export type TaskChannel = 'self_use' | 'cross_border' | 'manual';
export type TaskExecutor = 'auto' | 'manual';
export type TaskStatus = 'pending' | 'executing' | 'ordered' | 'paid' | 'shipped' | 'closed' | 'exception';

/** 任务载荷快照（payload，下单选参数时记的；列表行不带 posting/store 字段，从这解）。 */
export interface TaskPayloadDTO {
  store_id: string;
  posting_number: string;
  relay_point_id: string;
  relay_name: string;
  relay_address: string;
  relay_contact: string;
  ship_deadline: string | null;
  currency: string;
  items: {
    ozon_offer_id: string;
    item_id: string;
    sku_id: string;
    url: string;
    qty: number;
    unit_price: string;
    currency: string;
  }[];
  note?: string;
}

export interface PurchaseTaskDTO {
  id: string;
  order_id: string;
  supplier_offer_id: string | null;
  channel: TaskChannel;
  executor_type: TaskExecutor;
  status: TaskStatus;
  payload: TaskPayloadDTO | null;
  assignee: string | null;
  deadline: string | null;
  idempotency_key: string | null;
  created_at: string;
  updated_at: string;
}

/** 采购单（实际下单结果；列表接口不带，详情接口带）。 */
export interface PurchaseOrderDTO {
  id: string;
  task_id: string;
  platform_order_id: string;
  amount: string;
  currency: string;
  paid_at: string | null;
  domestic_carrier: string | null;
  domestic_tracking_no: string | null;
}

export interface PurchaseTaskListQuery {
  order_id?: string;
  store_id?: string;
  status?: string;
  executor_type?: string;
  assignee?: string;
  page: number;
  page_size: number;
}

export interface PurchaseTaskListData {
  total: number;
  items: PurchaseTaskDTO[];
}

/** 备料单（总纲 §7.3）：收货地址 = 中转点、备注含 posting_number、不含买家个人信息。 */
export interface MaterialSheetDTO {
  task_id: string;
  posting_number: string;
  receiver: string; // 中转点名称
  address: string;
  contact: string;
  deadline: string | null;
  note: string;
  items: { item_id: string; sku_id: string; url: string; qty: number; price: string; currency: string }[];
  text: string; // 人读版：复制给采购同事照着下单
}

export interface PurchaseTaskDetailData {
  task: PurchaseTaskDTO;
  purchase_order: PurchaseOrderDTO | null;
  material_sheet: MaterialSheetDTO | null;
}

/** 记已付款（后端只收 amount + paid_at；odered → paid）。 */
export interface MarkPaidReq {
  amount: string;
  paid_at?: string;
}

/**
 * 回填（人工执行器 / 自动任务补国内快递号）。
 * 后端校验：国内快递号必填（6–32 位字母数字或连字符）；平台单号/实付给了才校格式，
 * 且采购单里还没有这两项时必须补上（backend/internal/purchase/manual.go:97-108,127-131）。
 */
export interface FillBackReq {
  domestic_tracking_no: string;
  platform_order_id?: string;
  amount?: string;
  domestic_carrier?: string;
  paid_at?: string;
}

export function listPurchaseTasks(q: PurchaseTaskListQuery): Promise<PurchaseTaskListData> {
  return request<PurchaseTaskListData>({ method: 'GET', url: '/api/purchase-tasks', params: q });
}

export function getPurchaseTask(id: string): Promise<PurchaseTaskDetailData> {
  return request<PurchaseTaskDetailData>({ method: 'GET', url: `/api/purchase-tasks/${id}` });
}

/** 手动触发自动执行器（1688 预览 → 下单）；不可执行时后端明确报错。 */
export function executePurchaseTask(id: string): Promise<null> {
  return request<null>({ method: 'POST', url: `/api/purchase-tasks/${id}/execute` });
}

/** 转人工（新商家首单、通道不可用时的人工兜底）。 */
export function convertManualPurchaseTask(
  id: string,
  body?: { assignee?: string; note?: string },
): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({
    method: 'POST',
    url: `/api/purchase-tasks/${id}/convert-manual`,
    data: body ?? {},
  });
}

/** 记已付款（人工在 1688 付款后回填实付，任务 ordered → paid）。 */
export function markPaidPurchaseTask(id: string, body: MarkPaidReq): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/mark-paid`, data: body });
}

/** 回填平台单号 / 实付 / 国内快递号（国内快递号不回传 Ozon）。 */
export function fillBackPurchaseTask(id: string, body: FillBackReq): Promise<PurchaseTaskDTO> {
  return request<PurchaseTaskDTO>({ method: 'POST', url: `/api/purchase-tasks/${id}/fill-back`, data: body });
}

export function getMaterialSheet(id: string): Promise<MaterialSheetDTO> {
  return request<MaterialSheetDTO>({ method: 'GET', url: `/api/purchase-tasks/${id}/material-sheet` });
}
