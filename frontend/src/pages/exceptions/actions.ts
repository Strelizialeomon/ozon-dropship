import { getDefaultStore, useSetAtom } from 'jotai';
import { listExceptions, resolveException } from '@/api/exceptions';
import {
  type ExceptionFilters,
  exceptionFiltersAtom,
  exceptionsAtom,
  exceptionsLoadingAtom,
  exceptionsTotalAtom,
  exceptionSubmittingAtom,
} from './store';

let reqSeq = 0;

export function useExceptionActions() {
  const setList = useSetAtom(exceptionsAtom);
  const setTotal = useSetAtom(exceptionsTotalAtom);
  const setLoading = useSetAtom(exceptionsLoadingAtom);
  const setFilters = useSetAtom(exceptionFiltersAtom);
  const setSubmitting = useSetAtom(exceptionSubmittingAtom);

  async function loadExceptions(q: ExceptionFilters): Promise<void> {
    const id = ++reqSeq;
    setLoading(true);
    try {
      const data = await listExceptions(q);
      if (id !== reqSeq) return;
      setList(data.items);
      setTotal(data.total);
      const pageCount = Math.max(1, Math.ceil(data.total / q.page_size));
      if (q.page > pageCount) {
        void loadExceptions({ ...q, page: pageCount });
      }
    } catch {
      // 拦截器已提示
    } finally {
      if (id === reqSeq) setLoading(false);
    }
  }

  /** 用「此刻」的筛选条件重拉（处理完一条后刷新用）。 */
  async function reloadExceptions(): Promise<void> {
    await loadExceptions(getDefaultStore().get(exceptionFiltersAtom));
  }

  function applyFilters(patch: Partial<ExceptionFilters>): void {
    let next!: ExceptionFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadExceptions(next);
  }

  function changePage(page: number): void {
    let next!: ExceptionFilters;
    setFilters((prev) => {
      next = { ...prev, page };
      return next;
    });
    void loadExceptions(next);
  }

  async function resolve(id: string, note: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await resolveException(id, note);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  return { loadExceptions, reloadExceptions, applyFilters, changePage, resolve };
}
