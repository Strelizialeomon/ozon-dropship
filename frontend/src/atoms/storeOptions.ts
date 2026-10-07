// 跨 feature 共享：「店铺下拉选项」。订单筛选、映射表单、打包页都要它，
// 多处各自拉一次会重复请求——这里缓存一份（店铺列表变更频率低，刷新页面即重拉）。

import { atom, useSetAtom } from 'jotai';
import { listStores, type StoreDTO } from '@/api/stores';

export const storeOptionsAtom = atom<StoreDTO[]>([]);

let inflight: Promise<StoreDTO[]> | null = null;

export function useStoreOptionsActions() {
  const setStores = useSetAtom(storeOptionsAtom);

  async function ensureStores(): Promise<StoreDTO[]> {
    inflight ??= listStores();
    try {
      const list = await inflight;
      setStores(list);
      return list;
    } catch {
      inflight = null; // 失败允许下次重试
      return [];
    }
  }

  return { ensureStores };
}
