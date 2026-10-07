// 异常池（S1-D 的接口清单：/api/exceptions —— 列表 / 处理）。⚠️ 待 D 落地对齐。
import { request } from './client';

export type ExceptionRefType = 'order' | 'purchase_task' | 'shipment';

export interface ExceptionDTO {
  id: string;
  ref_type: ExceptionRefType;
  ref_id: string;
  code: string; // 总纲 §5.2 判定清单（purchase_timeout / ship_failed / arbitration …）
  detail: string;
  status: 'open' | 'resolved';
  handled_by: string | null;
  handled_at: string | null;
  note: string | null;
  created_at: string;
}

export interface ExceptionListQuery {
  page: number;
  page_size: number;
  status?: string;
  ref_type?: string;
  code?: string;
}

export interface ExceptionListData {
  total: number;
  list: ExceptionDTO[];
}

export function listExceptions(q: ExceptionListQuery): Promise<ExceptionListData> {
  return request<ExceptionListData>({ method: 'GET', url: '/api/exceptions', params: q });
}

/** 标记已处理（同一对象同一 code 未处理时去重，处理掉才允许再进池）。 */
export function resolveException(id: string, note: string): Promise<ExceptionDTO> {
  return request<ExceptionDTO>({ method: 'POST', url: `/api/exceptions/${id}/resolve`, data: { note } });
}
