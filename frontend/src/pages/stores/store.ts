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

/** 按店过滤（'' = 全部）。 */
export const credentialStoreFilterAtom = atom<string>('');

/** 受控弹窗：新建（可预置店/种类）或轮换某条已有凭据。 */
export type CredentialModal = null | { mode: 'create'; storeId?: string } | {
  mode: 'rotate';
  credential: CredentialDTO;
};
export const credentialModalAtom = atom<CredentialModal>(null);
