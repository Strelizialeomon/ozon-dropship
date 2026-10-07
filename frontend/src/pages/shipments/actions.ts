import { useSetAtom } from 'jotai';
import {
  createRelayPoint as apiCreateRelay,
  deleteRelayPoint as apiDeleteRelay,
  getShipmentLabel,
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
        stage: q.stage || undefined,
        keyword: q.keyword || undefined,
      });
      if (id !== shipSeq) return;
      setList(data.list);
      setTotal(data.total);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === shipSeq) setLoading(false);
    }
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

  /** 备货贴单交运 → handed_over（后端复核 substatus）。 */
  async function ship(orderId: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await shipShipment(orderId);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  /** 传单号（仅 3pl_tracking / non_integrated）。 */
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

  /** 面单：拿 URL 后新开窗口（PDF）。 */
  async function openLabel(orderId: string): Promise<void> {
    const { url } = await getShipmentLabel(orderId);
    window.open(url, '_blank', 'noopener');
  }

  return { loadShipments, applyFilters, changePage, receive, ship, setTracking, openLabel };
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
