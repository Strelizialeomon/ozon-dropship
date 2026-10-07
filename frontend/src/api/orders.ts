// 订单工作台接口（后端已实现：backend/internal/order/handler.go、model.go）。
// 总纲 §4.1：接口以后端 Go 代码为准、前端手写——本文件字段逐个照 Go struct 的 json tag。
import { request } from './client';

/** 订单行（orders 表；posting 粒度）。 */
export interface OrderDTO {
  id: string;
  store_id: string;
  posting_number: string;
  order_number: string;
  parent_posting_number: string | null;
  status: string; // 总纲 §5.2 内部状态机
  ozon_status: string;
  ozon_substatus: string | null;
  tpl_integration_type: string | null;
  ship_deadline: string | null;
  relay_point_id: string | null;
  total_amount: string; // decimal 走字符串
  currency: string;
  created_at: string;
  updated_at: string;
}

/** 订单行明细（order_items 表；详情接口带）。 */
export interface OrderItemDTO {
  id: string;
  order_id: string;
  ozon_offer_id: string;
  qty: number;
  price: string;
  currency: string;
  offer_link_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface OrderListQuery {
  store_id?: string;
  status?: string; // 可逗号分隔多值（后端 splitCSV）
  ozon_status?: string;
  tpl_integration_type?: string;
  keyword?: string;
  from?: string; // RFC3339
  to?: string;
  page: number;
  page_size: number;
}

export interface OrderListData {
  total: number;
  items: OrderDTO[];
}

/** GET /api/orders/:id → 订单 + 商品行。 */
export interface OrderDetailData {
  order: OrderDTO;
  items: OrderItemDTO[];
}

/** 批量动作（后端 oneof）；每条独立成败，不整批回滚。 */
export type OrderBatchAction = 'plan_purchase' | 'set_relay_point';

export interface OrderBatchFailure {
  id: string;
  error: string;
}

export interface OrderBatchResult {
  succeeded: number;
  failed: OrderBatchFailure[];
}

export function listOrders(q: OrderListQuery): Promise<OrderListData> {
  return request<OrderListData>({ method: 'GET', url: '/api/orders', params: q });
}

export function getOrder(id: string): Promise<OrderDetailData> {
  return request<OrderDetailData>({ method: 'GET', url: `/api/orders/${id}` });
}

export function batchOrders(ids: string[], action: OrderBatchAction, relayPointId?: string): Promise<OrderBatchResult> {
  return request<OrderBatchResult>({
    method: 'POST',
    url: '/api/orders/batch',
    data: { ids, action, relay_point_id: relayPointId ?? '' },
  });
}
