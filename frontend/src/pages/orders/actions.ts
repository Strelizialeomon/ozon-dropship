import { useSetAtom } from 'jotai';
import { batchOrders, listOrders, type OrderBatchResult } from '@/api/orders';
import {
  orderBatchSubmittingAtom,
  type OrderFilters,
  orderFiltersAtom,
  ordersAtom,
  orderSelectionAtom,
  ordersLoadingAtom,
  ordersTotalAtom,
} from './store';

// 竞态保护（hi-jotai 招牌习惯）：连续筛选/翻页时只让最后一个响应写 atom。
let reqSeq = 0;

/** OrderFilters 的字段是 OrderListQuery 的子集（非可选 vs 可选），可直接透传给 api。 */
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
      setOrders(data.list);
      setTotal(data.total);
      setSelection({}); // 换页/换筛选后旧勾选不再对应可见行
    } catch {
      // 拦截器已提示；保持旧数据
    } finally {
      if (id === reqSeq) setLoading(false);
    }
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

  return { loadOrders, applyFilters, changePage, resetFilters };
}

export function useOrderBatchActions() {
  const setSubmitting = useSetAtom(orderBatchSubmittingAtom);

  /** 批量生成采购任务（总纲 §5.1：平台未放行 / 已有任务的后端会跳过并计数）。 */
  async function createPurchaseTasks(ids: string[]): Promise<OrderBatchResult | null> {
    setSubmitting(true);
    try {
      return await batchOrders(ids, 'create_purchase_task');
    } catch {
      return null; // 拦截器已提示
    } finally {
      setSubmitting(false);
    }
  }

  return { createPurchaseTasks };
}
