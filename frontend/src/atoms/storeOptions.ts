// 跨 feature 共享：「店铺下拉选项」。订单筛选、映射表单、打包页都要它，
// 多处各自拉一次会重复请求——这里缓存一份。
//
// 缓存失效：店铺增删改后由「店铺与凭据」页调 `invalidateStoreOptions()`（再配合
// refreshStores 立即重拉），否则新建的店在下拉里看不到（评审路二 #8）。

import { atom, useSetAtom } from 'jotai';
import { listStores, type StoreDTO } from '@/api/stores';

export const storeOptionsAtom = atom<StoreDTO[]>([]);

let inflight: Promise<StoreDTO[]> | null = null;

/** 店铺数据变了之后调用：丢掉缓存，下一次 ensureStores 重新拉。 */
export function invalidateStoreOptions(): void {
  inflight = null;
}

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

  /** 强制重拉（无视缓存）：店铺增删改后用。 */
  async function refreshStores(): Promise<StoreDTO[]> {
    inflight = null;
    return ensureStores();
  }

  return { ensureStores, refreshStores };
}
