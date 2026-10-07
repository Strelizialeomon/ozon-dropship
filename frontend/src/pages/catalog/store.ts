import { atom } from 'jotai';
import type { OfferLinkDTO, SupplierOfferDTO } from '@/api/catalog';

export const catalogTabAtom = atom<'offers' | 'links'>('offers');

// ── 货源商品（supplier_offers；后端列表不分页）──────────────────────────────
export interface OfferFilters {
  platform: string;
  status: string;
  keyword: string;
}

export const offerFiltersAtom = atom<OfferFilters>({ platform: '', status: '', keyword: '' });
export const offersAtom = atom<SupplierOfferDTO[]>([]);
export const offersLoadingAtom = atom<boolean>(false);

export type OfferModal = null | { mode: 'create' } | { mode: 'edit'; offer: SupplierOfferDTO };
export const offerModalAtom = atom<OfferModal>(null);
export const offerDeletingAtom = atom<SupplierOfferDTO | null>(null);

// ── 按店映射（offer_links；后端列表不分页）─────────────────────────────────
export interface LinkFilters {
  store_id: string;
  ozon_offer_id: string;
}

export const linkFiltersAtom = atom<LinkFilters>({ store_id: '', ozon_offer_id: '' });
export const linksAtom = atom<OfferLinkDTO[]>([]);
export const linksLoadingAtom = atom<boolean>(false);

export type LinkModal = null | { mode: 'create' } | { mode: 'edit'; link: OfferLinkDTO };
export const linkModalAtom = atom<LinkModal>(null);
export const linkDeletingAtom = atom<OfferLinkDTO | null>(null);

export const catalogSubmittingAtom = atom<boolean>(false);
