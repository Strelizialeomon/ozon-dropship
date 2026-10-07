import { atom } from 'jotai';
import type { MaterialSheetDTO, PurchaseTaskDTO } from '@/api/purchase-tasks';

export interface TaskFilters {
  store_id: string; // '' = 全部（后端 list 支持 store_id）
  status: string;
  executor_type: string;
  page: number;
  page_size: number;
}

export const taskFiltersAtom = atom<TaskFilters>({
  store_id: '',
  status: '',
  executor_type: '',
  page: 1,
  page_size: 20,
});
export const tasksAtom = atom<PurchaseTaskDTO[]>([]);
export const tasksTotalAtom = atom<number>(0);
export const tasksLoadingAtom = atom<boolean>(false);

/** 受控目标：null = 关。 */
export const taskExecutingTargetAtom = atom<PurchaseTaskDTO | null>(null); // 立即执行确认
export const taskMarkPaidTargetAtom = atom<PurchaseTaskDTO | null>(null);
export const taskBackfillTargetAtom = atom<PurchaseTaskDTO | null>(null);
export const taskToManualTargetAtom = atom<PurchaseTaskDTO | null>(null);

/** 备料单（含加载态：打开即拉）。 */
export const taskPrepSheetAtom = atom<MaterialSheetDTO | null>(null);
export const taskActionSubmittingAtom = atom<boolean>(false);
