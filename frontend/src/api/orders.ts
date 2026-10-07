// 订单工作台（S1-D 的接口清单：/api/orders —— 列表 / 筛选 / 详情 / 批量）。
//
// ⚠️ D 尚未合并：本文件按子 spec spec-s1d-fulfillment §3 + 总纲 §6 字段手写契约。
// D 落地后以后端 Go handler 为准核对一遍，不符以内端为准并同步此文件（总纲 §4.1：
// 接口以后端 Go 代码为准，前端手写调用）。字段名与总纲 §6 保持一致。
import { request } from './client';

export interface OrderItemDTO {
  id: string;
  ozon_offer_id: string;
  qty: number;
  price: string; // DECIMAL 以字符串给到前端
  currency: string;
  offer_link_id: string | null;
}

export interface OrderDTO {
  id: string;
  store_id: string;
  store_name: string;
  posting_number: string;
  order_number: string;
  parent_posting_number: string | null;
  status: string; // 总纲 §5.2 内部状态机
  ozon_status: string;
  ozon_substatus: string | null;
  tpl_integration_type: string;
  ship_deadline: string | null;
  relay_point_id: string | null;
  relay_point_name?: string | null;
  currency: string;
  total_amount: string | null; // 单内商品合计（明细之和）
  items: OrderItemDTO[];
  created_at: string;
  updated_at: string;
}

export interface OrderListQuery {
  page: number;
  page_size: number;
  store_id?: string;
  status?: string;
  keyword?: string; // posting_number / order_number 模糊
}

export interface OrderListData {
  total: number;
  list: OrderDTO[];
}

/** 批量动作：给选中的订单生成采购任务（总纲 §5.1；已在执行中的跳过）。 */
export type OrderBatchAction = 'create_purchase_task';

export interface OrderBatchResult {
  acted: number;
  skipped: number; // 平台未放行 / 已有任务等
  messages?: string[];
}

export function listOrders(q: OrderListQuery): Promise<OrderListData> {
  return request<OrderListData>({ method: 'GET', url: '/api/orders', params: q });
}

export function getOrder(id: string): Promise<OrderDTO> {
  return request<OrderDTO>({ method: 'GET', url: `/api/orders/${id}` });
}

export function batchOrders(ids: string[], action: OrderBatchAction): Promise<OrderBatchResult> {
  return request<OrderBatchResult>({ method: 'POST', url: '/api/orders/batch', data: { ids, action } });
}
