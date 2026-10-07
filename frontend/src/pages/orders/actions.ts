import { getDefaultStore, useSetAtom } from 'jotai';
import { batchOrders, getOrder, listOrders, type OrderBatchResult } from '@/api/orders';
import {
  orderBatchSubmittingAtom,
  orderDetailAtom,
  orderDetailLoadingAtom,
  type OrderFilters,
  orderFiltersAtom,
  ordersAtom,
  orderSelectionAtom,
  ordersLoadingAtom,
  ordersTotalAtom,
} from './store';

// 竞态保护（hi-jotai 招牌习惯）：连续筛选/翻页时只让最后一个响应写 atom。
let reqSeq = 0;

/** OrderFilters 的字段是 OrderListQuery 的子集，可直接透传给 api。 */
export function useOrderListActions() {
  const setOrders = useSetAtom(ordersAtom);
  const setTotal = useSetAtom(ordersTotalAtom);
  const setLoading = useSetAtom(ordersLoadingAtom);
  const setFilters = useSetAtom(orderFiltersAtom);
  const setSelection = useSetAtom(orderSelectionAtom);

  async function loadOrders(q: OrderFilters): Promise<void> {
    const id = ++reqSeq;
    setLoading(true);
    try {
      const data = await listOrders(q);
      if (id !== reqSeq) return; // 过期响应，丢弃
      setOrders(data.items);
      setTotal(data.total);
      setSelection({}); // 换页/换筛选后旧勾选不再对应可见行
      // 页码越界回收：处理完最后一页的最后一条后 total 变小，别停在空页上。
      const pageCount = Math.max(1, Math.ceil(data.total / q.page_size));
      if (q.page > pageCount) {
        void loadOrders({ ...q, page: pageCount });
      }
    } catch {
      // 拦截器已提示；保持旧数据
    } finally {
      if (id === reqSeq) setLoading(false);
    }
  }

  /** 用「此刻」的筛选条件重拉（动作后刷新用；别拿渲染时的 filters 快照）。 */
  async function reloadOrders(): Promise<void> {
    await loadOrders(getDefaultStore().get(orderFiltersAtom));
  }

  /** 合并筛选条件并回第 1 页重查。 */
  function applyFilters(patch: Partial<OrderFilters>): void {
    let next!: OrderFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadOrders(next);
  }

  function changePage(page: number): void {
    let next!: OrderFilters;
    setFilters((prev) => {
      next = { ...prev, page };
      return next;
    });
    void loadOrders(next);
  }

  function resetFilters(): void {
    const next: OrderFilters = { store_id: '', status: '', keyword: '', page: 1, page_size: 20 };
    setFilters(next);
    void loadOrders(next);
  }

  return { loadOrders, reloadOrders, applyFilters, changePage, resetFilters };
}

export function useOrderBatchActions() {
  const setSubmitting = useSetAtom(orderBatchSubmittingAtom);

  /** 批量生成采购任务（后端 action=plan_purchase；每条独立成败）。 */
  async function planPurchase(ids: string[]): Promise<OrderBatchResult | null> {
    setSubmitting(true);
    try {
      return await batchOrders(ids, 'plan_purchase');
    } catch {
      return null; // 拦截器已提示
    } finally {
      setSubmitting(false);
    }
  }

  return { planPurchase };
}

export function useOrderDetailActions() {
  const setDetail = useSetAtom(orderDetailAtom);
  const setLoading = useSetAtom(orderDetailLoadingAtom);

  /** 打开详情：列表行没有商品行，按 ID 拉一次 GET /api/orders/:id。 */
  async function loadOrderDetail(id: string): Promise<void> {
    setDetail(null);
    setLoading(true);
    try {
      setDetail(await getOrder(id));
    } catch {
      // 拦截器已提示
    } finally {
      setLoading(false);
    }
  }

  return { loadOrderDetail };
}
