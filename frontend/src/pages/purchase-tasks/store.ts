import { atom } from 'jotai';
import type { PrepSheetDTO, PurchaseTaskDTO } from '@/api/purchase-tasks';

export interface TaskFilters {
  status: string; // '' = 全部
  channel: string;
  executor_type: string;
  keyword: string;
  page: number;
  page_size: number;
}

export const taskFiltersAtom = atom<TaskFilters>({
  status: '',
  channel: '',
  executor_type: '',
  keyword: '',
  page: 1,
  page_size: 20,
});
export const tasksAtom = atom<PurchaseTaskDTO[]>([]);
export const tasksTotalAtom = atom<number>(0);
export const tasksLoadingAtom = atom<boolean>(false);

/** 三个受控目标：null = 关。 */
export const taskExecutingTargetAtom = atom<PurchaseTaskDTO | null>(null); // 立即执行确认
export const taskMarkPaidTargetAtom = atom<PurchaseTaskDTO | null>(null);
export const taskBackfillTargetAtom = atom<PurchaseTaskDTO | null>(null);
export const taskToManualTargetAtom = atom<PurchaseTaskDTO | null>(null);

/** 备料单（含加载态：打开即拉）。 */
export const taskPrepSheetAtom = atom<PrepSheetDTO | null>(null);
export const taskActionSubmittingAtom = atom<boolean>(false);
