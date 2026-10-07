import { getDefaultStore, useSetAtom } from 'jotai';
import {
  createRelayPoint as apiCreateRelay,
  deleteRelayPoint as apiDeleteRelay,
  fetchShipmentLabel,
  listRelayPoints,
  listShipments,
  receiveShipment,
  type RelayPointReq,
  setShipmentTracking,
  shipShipment,
  updateRelayPoint as apiUpdateRelay,
} from '@/api/shipments';
import {
  relayPointsAtom,
  relayPointsLoadingAtom,
  type ShipmentFilters,
  shipmentFiltersAtom,
  shipmentsAtom,
  shipmentsLoadingAtom,
  shipmentsTotalAtom,
  shipmentSubmittingAtom,
} from './store';

let shipSeq = 0;

export function useShipmentActions() {
  const setList = useSetAtom(shipmentsAtom);
  const setTotal = useSetAtom(shipmentsTotalAtom);
  const setLoading = useSetAtom(shipmentsLoadingAtom);
  const setFilters = useSetAtom(shipmentFiltersAtom);
  const setSubmitting = useSetAtom(shipmentSubmittingAtom);

  async function loadShipments(q: ShipmentFilters): Promise<void> {
    const id = ++shipSeq;
    setLoading(true);
    try {
      const data = await listShipments({
        page: q.page,
        page_size: q.page_size,
        relay_point_id: q.relay_point_id || undefined,
        status: q.stage || undefined,
      });
      if (id !== shipSeq) return;
      setList(data.items);
      setTotal(data.total);
      const pageCount = Math.max(1, Math.ceil(data.total / q.page_size));
      if (q.page > pageCount) {
        void loadShipments({ ...q, page: pageCount });
      }
    } catch {
      // 拦截器已提示
    } finally {
      if (id === shipSeq) setLoading(false);
    }
  }

  /** 用「此刻」的筛选条件重拉（动作后刷新用）。 */
  async function reloadShipments(): Promise<void> {
    await loadShipments(getDefaultStore().get(shipmentFiltersAtom));
  }

  function applyFilters(patch: Partial<ShipmentFilters>): void {
    let next!: ShipmentFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadShipments(next);
  }

  function changePage(page: number): void {
    let next!: ShipmentFilters;
    setFilters((prev) => {
      next = { ...prev, page };
      return next;
    });
    void loadShipments(next);
  }

  /** 中转点签收 → at_relay。 */
  async function receive(orderId: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await receiveShipment(orderId);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  /** 备货贴单交运 → handed_over（后端复核 substatus；失败会带原因报错）。 */
  async function ship(orderId: string): Promise<{ ok: boolean; notes?: string[] }> {
    setSubmitting(true);
    try {
      const res = await shipShipment(orderId);
      return { ok: true, notes: res.notes ?? [] };
    } catch {
      return { ok: false };
    } finally {
      setSubmitting(false);
    }
  }

  /** 传单号（仅 tracking_action = set 的行）。 */
  async function setTracking(orderId: string, trackingNo: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await setShipmentTracking(orderId, trackingNo);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  /**
   * 面单：先同步开一个空窗口（保住用户手势，防弹窗拦截），再拿 PDF blob 换进去；
   * 失败时关掉空窗口，错误提示由拦截器兜底（评审路二 #13）。
   */
  async function openLabel(orderId: string): Promise<void> {
    const win = window.open('', '_blank');
    try {
      const blob = await fetchShipmentLabel(orderId);
      const url = URL.createObjectURL(blob);
      if (win) {
        win.location.href = url;
      } else {
        // 空窗口被拦：退化为直接开 blob URL（可能被拦，用户可再点一次）
        window.open(url, '_blank');
      }
      // blob URL 交给新窗口使用，延迟回收
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (e) {
      win?.close();
      throw e;
    }
  }

  return { loadShipments, reloadShipments, applyFilters, changePage, receive, ship, setTracking, openLabel };
}

export function useRelayPointActions() {
  const setList = useSetAtom(relayPointsAtom);
  const setLoading = useSetAtom(relayPointsLoadingAtom);

  async function loadRelayPoints(): Promise<void> {
    setLoading(true);
    try {
      setList(await listRelayPoints());
    } catch {
      // 拦截器已提示
    } finally {
      setLoading(false);
    }
  }

  async function createRelayPoint(req: RelayPointReq): Promise<boolean> {
    try {
      await apiCreateRelay(req);
      await loadRelayPoints();
      return true;
    } catch {
      return false;
    }
  }

  async function updateRelayPoint(id: string, req: RelayPointReq): Promise<boolean> {
    try {
      await apiUpdateRelay(id, req);
      await loadRelayPoints();
      return true;
    } catch {
      return false;
    }
  }

  async function removeRelayPoint(id: string): Promise<boolean> {
    try {
      await apiDeleteRelay(id);
      await loadRelayPoints();
      return true;
    } catch {
      return false;
    }
  }

  return { loadRelayPoints, createRelayPoint, updateRelayPoint, removeRelayPoint };
}
