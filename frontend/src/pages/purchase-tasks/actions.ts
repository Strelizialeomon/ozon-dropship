import { useSetAtom } from 'jotai';
import {
  backfillPurchaseTask,
  type BackfillReq,
  executePurchaseTask,
  getPrepSheet,
  listPurchaseTasks,
  markPaidPurchaseTask,
  type MarkPaidReq,
  toManualPurchaseTask,
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
      setTasks(data.list);
      setTotal(data.total);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === reqSeq) setLoading(false);
    }
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
    const next: TaskFilters = { status: '', channel: '', executor_type: '', keyword: '', page: 1, page_size: 20 };
    setFilters(next);
    void loadTasks(next);
  }

  return { loadTasks, applyFilters, changePage, resetFilters };
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

  async function toManual(id: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await toManualPurchaseTask(id);
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

  async function backfill(id: string, body: BackfillReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await backfillPurchaseTask(id, body);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function openPrepSheet(id: string): Promise<void> {
    try {
      setPrepSheet(await getPrepSheet(id));
    } catch {
      // 拦截器已提示
    }
  }

  return { execute, toManual, markPaid, backfill, openPrepSheet };
}
