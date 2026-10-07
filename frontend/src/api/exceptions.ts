// 异常池接口（后端已实现：backend/internal/order/exception.go）。
import { request } from './client';

export type ExceptionRefType = 'order' | 'purchase_task' | 'shipment';

export interface ExceptionDTO {
  id: string;
  ref_type: ExceptionRefType;
  ref_id: string;
  code: string; // 后端常量见 order/exception.go:34-47
  detail: string | null;
  status: 'open' | 'resolved';
  handled_by: string | null;
  handled_at: string | null;
  note: string | null;
  created_at: string;
  updated_at: string;
}

export interface ExceptionListQuery {
  ref_type?: string;
  ref_id?: string;
  code?: string;
  /** 不传 = open（后端默认只看未处理）；'all' = 全部。 */
  status?: string;
  page: number;
  page_size: number;
}

export interface ExceptionListData {
  total: number;
  items: ExceptionDTO[];
}

export function listExceptions(q: ExceptionListQuery): Promise<ExceptionListData> {
  return request<ExceptionListData>({ method: 'GET', url: '/api/exceptions', params: q });
}

/** 标记已处理（备注可空；已处理再点一次不报错）。 */
export function resolveException(id: string, note: string): Promise<ExceptionDTO> {
  return request<ExceptionDTO>({ method: 'POST', url: `/api/exceptions/${id}/resolve`, data: { note } });
}
