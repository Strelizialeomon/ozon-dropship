import { getDefaultStore, useSetAtom } from 'jotai';
import {
  convertManualPurchaseTask,
  executePurchaseTask,
  fillBackPurchaseTask,
  type FillBackReq,
  getMaterialSheet,
  listPurchaseTasks,
  markPaidPurchaseTask,
  type MarkPaidReq,
} from '@/api/purchase-tasks';
import {
  taskActionSubmittingAtom,
  type TaskFilters,
  taskFiltersAtom,
  taskPrepSheetAtom,
  tasksAtom,
  tasksLoadingAtom,
  tasksTotalAtom,
} from './store';

let reqSeq = 0;
let prepSeq = 0; // 备料单竞态：连点两行时只让最后一次的响应上屏

export function useTaskListActions() {
  const setTasks = useSetAtom(tasksAtom);
  const setTotal = useSetAtom(tasksTotalAtom);
  const setLoading = useSetAtom(tasksLoadingAtom);
  const setFilters = useSetAtom(taskFiltersAtom);

  async function loadTasks(q: TaskFilters): Promise<void> {
    const id = ++reqSeq;
    setLoading(true);
    try {
      const data = await listPurchaseTasks(q);
      if (id !== reqSeq) return;
      setTasks(data.items);
      setTotal(data.total);
      const pageCount = Math.max(1, Math.ceil(data.total / q.page_size));
      if (q.page > pageCount) {
        void loadTasks({ ...q, page: pageCount });
      }
    } catch {
      // 拦截器已提示
    } finally {
      if (id === reqSeq) setLoading(false);
    }
  }

  /** 用「此刻」的筛选条件重拉（动作后刷新用）。 */
  async function reloadTasks(): Promise<void> {
    await loadTasks(getDefaultStore().get(taskFiltersAtom));
  }

  function applyFilters(patch: Partial<TaskFilters>): void {
    let next!: TaskFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadTasks(next);
  }

  function changePage(page: number): void {
    let next!: TaskFilters;
    setFilters((prev) => {
      next = { ...prev, page };
      return next;
    });
    void loadTasks(next);
  }

  /** 重置筛选条件。 */
  function resetFilters(): void {
    const next: TaskFilters = { store_id: '', status: '', executor_type: '', page: 1, page_size: 20 };
    setFilters(next);
    void loadTasks(next);
  }

  return { loadTasks, reloadTasks, applyFilters, changePage, resetFilters };
}

export function useTaskActions() {
  const setSubmitting = useSetAtom(taskActionSubmittingAtom);
  const setPrepSheet = useSetAtom(taskPrepSheetAtom);

  async function execute(id: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await executePurchaseTask(id);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function toManual(id: string, note?: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await convertManualPurchaseTask(id, note ? { note } : undefined);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function markPaid(id: string, body: MarkPaidReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await markPaidPurchaseTask(id, body);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function fillBack(id: string, body: FillBackReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await fillBackPurchaseTask(id, body);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function openPrepSheet(id: string): Promise<void> {
    const seq = ++prepSeq;
    try {
      const sheet = await getMaterialSheet(id);
      if (seq !== prepSeq) return; // 过期响应，丢弃
      setPrepSheet(sheet);
    } catch {
      // 拦截器已提示
    }
  }

  return { execute, toManual, markPaid, fillBack, openPrepSheet };
}
