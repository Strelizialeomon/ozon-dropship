import { type RowSelectionState } from '@tanstack/react-table';
import { atom } from 'jotai';
import type { OrderDTO } from '@/api/orders';

export interface OrderFilters {
  store_id: string; // '' = 全部
  status: string; // '' = 全部
  keyword: string;
  page: number;
  page_size: number;
}

export const orderFiltersAtom = atom<OrderFilters>({ store_id: '', status: '', keyword: '', page: 1, page_size: 20 });
export const ordersAtom = atom<OrderDTO[]>([]);
export const ordersTotalAtom = atom<number>(0);
export const ordersLoadingAtom = atom<boolean>(false);

/** 勾选的行（rowId → true）；批量操作用。 */
export const orderSelectionAtom = atom<RowSelectionState>({});

/** 详情弹窗对象（null = 关）。 */
export const orderDetailAtom = atom<OrderDTO | null>(null);

export const orderBatchSubmittingAtom = atom<boolean>(false);
