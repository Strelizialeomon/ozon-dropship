import { type RowSelectionState } from '@tanstack/react-table';
import { atom } from 'jotai';
import type { OrderDetailData, OrderDTO } from '@/api/orders';

export interface OrderFilters {
  store_id: string; // '' = 全部
  status: string; // '' = 全部（单值；后端支持逗号分隔多值）
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

/** 详情弹窗（null = 关；打开时按需拉 GET /api/orders/:id 拿商品行）。 */
export const orderDetailAtom = atom<OrderDetailData | null>(null);
export const orderDetailLoadingAtom = atom<boolean>(false);

export const orderBatchSubmittingAtom = atom<boolean>(false);
