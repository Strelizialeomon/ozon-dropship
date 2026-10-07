import { atom } from 'jotai';
import type { RelayPointDTO, ShipmentListRowDTO } from '@/api/shipments';

export const shipmentTabAtom = atom<'handover' | 'relay-points'>('handover');

// ── 打包交接 ──────────────────────────────────────────────────────────────
export interface ShipmentFilters {
  relay_point_id: string; // '' = 全部
  /** 订单状态（后端按 status 过滤；'' = 全部）。 */
  stage: '' | 'inbound' | 'at_relay' | 'handed_over';
  page: number;
  page_size: number;
}

export const shipmentFiltersAtom = atom<ShipmentFilters>({ relay_point_id: '', stage: '', page: 1, page_size: 20 });
export const shipmentsAtom = atom<ShipmentListRowDTO[]>([]);
export const shipmentsTotalAtom = atom<number>(0);
export const shipmentsLoadingAtom = atom<boolean>(false);

/** 三个受控目标：签收 / 备货 / 传单号。 */
export const shipmentReceiveTargetAtom = atom<ShipmentListRowDTO | null>(null);
export const shipmentShipTargetAtom = atom<ShipmentListRowDTO | null>(null);
export const shipmentTrackingTargetAtom = atom<ShipmentListRowDTO | null>(null);
export const shipmentSubmittingAtom = atom<boolean>(false);

// ── 中转点 ────────────────────────────────────────────────────────────────
export const relayPointsAtom = atom<RelayPointDTO[]>([]);
export const relayPointsLoadingAtom = atom<boolean>(false);

export type RelayModal = null | { mode: 'create' } | { mode: 'edit'; point: RelayPointDTO };
export const relayModalAtom = atom<RelayModal>(null);
export const relayDeletingAtom = atom<RelayPointDTO | null>(null);
