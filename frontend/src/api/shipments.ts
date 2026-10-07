// 中转点与打包（S1-D 的接口清单：/api/relay-points、/api/shipments）——⚠️ 待 D 落地对齐。
// 交接链路（总纲 §5.7 / §7.4）：签收 → at_relay；备货（复核 substatus）→ handed_over；
// 面单取 Ozon 的 PDF；传单号只在 tpl_integration_type ∈ {3pl_tracking, non_integrated} 时发生。
import { request } from './client';

export type RelayKind = 'forwarder' | 'own_warehouse';

export interface RelayPointDTO {
  id: string;
  name: string;
  kind: RelayKind;
  address: string;
  contact: string;
  status: string;
}

export interface RelayPointReq {
  name: string;
  kind: RelayKind;
  address: string;
  contact: string;
  status?: string;
}

/** 打包交接行：一条订单（posting）在某一中转点的交接状态。 */
export interface ShipmentDTO {
  order_id: string;
  posting_number: string;
  store_id: string;
  store_name: string;
  order_status: string;
  relay_point_id: string | null;
  relay_point_name: string | null;
  ship_deadline: string | null;
  tpl_integration_type: string;
  tracking_no: string | null;
  tracking_source: 'ozon' | 'seller' | null;
  carrier: string | null;
  handed_over_at: string | null;
}

export interface ShipmentListQuery {
  page: number;
  page_size: number;
  relay_point_id?: string;
  stage?: 'inbound' | 'at_relay' | 'handed_over'; // 交接看板按阶段过滤
  keyword?: string;
}

export interface ShipmentListData {
  total: number;
  list: ShipmentDTO[];
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

/** 中转点签收（货代回传 / 自有仓扫码）→ at_relay。 */
export function receiveShipment(orderId: string): Promise<ShipmentDTO> {
  return request<ShipmentDTO>({ method: 'POST', url: `/api/shipments/${orderId}/receive` });
}

/** 备货贴单交运 → handed_over（后端复核 ship 结果 substatus，ship_failed 进异常池）。 */
export function shipShipment(orderId: string): Promise<ShipmentDTO> {
  return request<ShipmentDTO>({ method: 'POST', url: `/api/shipments/${orderId}/ship` });
}

/** 取 Ozon 面单 PDF（同步返回地址）。 */
export function getShipmentLabel(orderId: string): Promise<{ url: string }> {
  return request<{ url: string }>({ method: 'GET', url: `/api/shipments/${orderId}/label` });
}

/** 传承运商单号（仅 3pl_tracking / non_integrated 需要，由界面按 tpl_integration_type 决定是否露出）。 */
export function setShipmentTracking(orderId: string, trackingNo: string): Promise<ShipmentDTO> {
  return request<ShipmentDTO>({
    method: 'POST',
    url: `/api/shipments/${orderId}/tracking-number`,
    data: { tracking_no: trackingNo },
  });
}

/** 货代仓交接对照表（CSV，国内快递号 ↔ posting_number ↔ 面单）——直接用浏览器下载。 */
export function handoverExportUrl(relayPointId: string): string {
  return `/api/shipments/handover-export?relay_point_id=${encodeURIComponent(relayPointId)}`;
}
