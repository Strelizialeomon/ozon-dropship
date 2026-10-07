import { atom } from 'jotai';
import type { ExceptionDTO } from '@/api/exceptions';

/** 后端语义：不传 = 只看未处理（open）；'all' = 全部；'resolved' = 已处理。 */
export type ExceptionStatusFilter = 'open' | 'resolved' | 'all';

export interface ExceptionFilters {
  status: ExceptionStatusFilter;
  ref_type: string;
  code: string;
  page: number;
  page_size: number;
}

export const exceptionFiltersAtom = atom<ExceptionFilters>({
  status: 'open',
  ref_type: '',
  code: '',
  page: 1,
  page_size: 20,
});
export const exceptionsAtom = atom<ExceptionDTO[]>([]);
export const exceptionsTotalAtom = atom<number>(0);
export const exceptionsLoadingAtom = atom<boolean>(false);

/** 处理弹窗的目标（null = 关）+ 处理说明。 */
export const exceptionResolveTargetAtom = atom<ExceptionDTO | null>(null);
export const exceptionNoteAtom = atom<string>('');
export const exceptionSubmittingAtom = atom<boolean>(false);
