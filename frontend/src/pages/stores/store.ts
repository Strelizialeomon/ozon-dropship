import { atom } from 'jotai';
import type { CredentialDTO } from '@/api/credentials';
import type { StoreDTO } from '@/api/stores';

/** 页面自有 UI 微状态：当前 tab。 */
export const pageTabAtom = atom<'stores' | 'credentials'>('stores');

// ── 店铺 ─────────────────────────────────────────────────────────────────
export const storesAtom = atom<StoreDTO[]>([]);
export const storesLoadingAtom = atom<boolean>(false);

/** 受控弹窗：null = 关；create = 新建；edit = 编辑某一店。 */
export type StoreModal = null | { mode: 'create' } | { mode: 'edit'; store: StoreDTO };
export const storeModalAtom = atom<StoreModal>(null);

/** 删除确认的目标（null = 不显示确认框）。 */
export const storeDeletingAtom = atom<StoreDTO | null>(null);

// ── 凭据 ─────────────────────────────────────────────────────────────────
export const credentialsAtom = atom<CredentialDTO[]>([]);
export const credentialsLoadingAtom = atom<boolean>(false);

/** 按店过滤。哨兵值（评审路一/路二：空串不能同时当「企业级」和「未选」）：
 *  '_all' = 全部（后端不传参数）；'_ent' = 企业级（后端传空串）；其他 = 店 ID。 */
export const credentialStoreFilterAtom = atom<string>('_all');

/** 哨兵 → 后端参数。 */
export function credentialStoreParam(filter: string): string | undefined {
  if (filter === '_all') return undefined;
  if (filter === '_ent') return '';
  return filter;
}

/** 受控弹窗：新建（可预置店/种类）或轮换某条已有凭据。 */
export type CredentialModal = null | { mode: 'create'; storeId?: string } | {
  mode: 'rotate';
  credential: CredentialDTO;
};
export const credentialModalAtom = atom<CredentialModal>(null);
