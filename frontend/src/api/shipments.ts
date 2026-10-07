// 中转点与打包接口（后端已实现：backend/internal/shipment/handler.go、model.go）。
// 交接链路（总纲 §5.7 / §7.4）：签收 → at_relay；备货（复核 substatus）→ handed_over；
// 面单是 PDF 二进制；是否要传单号看行上的 tracking_action（none / set / block）。
import { ApiError, http, request, type RespShell } from './client';

export type RelayKind = 'forwarder' | 'own_warehouse';
export type RelayStatus = 'active' | 'disabled';

export interface RelayPointDTO {
  id: string;
  name: string;
  kind: RelayKind;
  address: string;
  contact: string;
  status: RelayStatus;
  created_at: string;
  updated_at: string;
}

export interface RelayPointReq {
  name: string;
  kind: RelayKind;
  address: string;
  contact: string;
  status?: RelayStatus;
}

/** 发运记录（shipments 表；国际段单号在这里）。 */
export interface ShipmentDTO {
  id: string;
  order_id: string;
  tracking_no: string | null;
  tracking_source: 'ozon' | 'seller' | null;
  carrier: string | null;
  label_ref: string | null;
  handed_over_at: string | null;
  created_at: string;
  updated_at: string;
}

/** 打包交接列表行（订单 + 发运 + 国内段单号 + 传单号动作判定）。 */
export interface ShipmentListRowDTO {
  order_id: string;
  store_id: string;
  posting_number: string;
  order_status: string;
  tpl_integration_type: string | null;
  ship_deadline: string | null;
  relay_point_id: string | null;
  domestic_carrier: string;
  domestic_tracking_no: string;
  shipment: ShipmentDTO | null;
  tracking_action: 'none' | 'set' | 'block';
}

export interface ShipmentListQuery {
  store_id?: string;
  relay_point_id?: string;
  status?: string; // 订单状态，可逗号分隔（后端 splitCSV）
  page: number;
  page_size: number;
}

export interface ShipmentListData {
  total: number;
  items: ShipmentListRowDTO[];
}

/** 备货结果（ShipResult）：跟踪动作与给操作员的提示。 */
export interface ShipResultDTO {
  order_status: string;
  tracking_action: 'none' | 'set' | 'block';
  notes: string[] | null;
}

export function listRelayPoints(): Promise<RelayPointDTO[]> {
  return request<RelayPointDTO[]>({ method: 'GET', url: '/api/relay-points' });
}

export function createRelayPoint(body: RelayPointReq): Promise<RelayPointDTO> {
  return request<RelayPointDTO>({ method: 'POST', url: '/api/relay-points', data: body });
}

export function updateRelayPoint(id: string, body: RelayPointReq): Promise<RelayPointDTO> {
  return request<RelayPointDTO>({ method: 'PUT', url: `/api/relay-points/${id}`, data: body });
}

export function deleteRelayPoint(id: string): Promise<null> {
  return request<null>({ method: 'DELETE', url: `/api/relay-points/${id}` });
}

export function listShipments(q: ShipmentListQuery): Promise<ShipmentListData> {
  return request<ShipmentListData>({ method: 'GET', url: '/api/shipments', params: q });
}

/** 签收 → at_relay（:id = 订单 ID）。 */
export function receiveShipment(orderId: string): Promise<ShipmentDTO> {
  return request<ShipmentDTO>({ method: 'POST', url: `/api/shipments/${orderId}/receive` });
}

/** 备货贴单交运 → handed_over（后端复核 substatus；ship_failed 进异常池并报错）。 */
export function shipShipment(orderId: string): Promise<ShipResultDTO> {
  return request<ShipResultDTO>({ method: 'POST', url: `/api/shipments/${orderId}/ship`, data: {} });
}

/** 传承运商单号（仅 tracking_action = set 的行需要）。 */
export function setShipmentTracking(orderId: string, trackingNo: string, carrier?: string): Promise<ShipmentDTO> {
  return request<ShipmentDTO>({
    method: 'POST',
    url: `/api/shipments/${orderId}/tracking`,
    data: { tracking_no: trackingNo, carrier: carrier ?? '' },
  });
}

/**
 * 面单 PDF（二进制）。走 axios 拿 blob，而不是直接 window.open：
 * 失败时（未备货 / Ozon 报错）后端尚业务错误壳，直接开窗只能看到一段 JSON。
 * ⚠️ 业务错误（HTTP 200）在 blob 模式下要手动解析出壳再抛 ApiError。
 */
export async function fetchShipmentLabel(orderId: string): Promise<Blob> {
  const resp = await http.get<Blob>(`/api/shipments/${orderId}/label`, { responseType: 'blob' });
  const data = resp.data;
  if (data.type.includes('application/json')) {
    const shell = JSON.parse(await data.text()) as RespShell;
    throw new ApiError(shell.code || 1, shell.error || shell.message || '取面单失败');
  }
  return data;
}

/** 货代仓交接对照表（CSV，浏览器直接下载）。 */
export function handoverExportUrl(relayPointId: string): string {
  return `/api/shipments/handover-export?relay_point_id=${encodeURIComponent(relayPointId)}`;
}
