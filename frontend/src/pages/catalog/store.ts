import { atom } from 'jotai';
import type { OfferLinkDTO, SupplierOfferDTO } from '@/api/catalog';

export const catalogTabAtom = atom<'offers' | 'links'>('offers');

// ── 货源商品（supplier_offers）─────────────────────────────────────────────
export interface OfferFilters {
  platform: string;
  keyword: string;
  page: number;
  page_size: number;
}

export const offerFiltersAtom = atom<OfferFilters>({ platform: '', keyword: '', page: 1, page_size: 20 });
export const offersAtom = atom<SupplierOfferDTO[]>([]);
export const offersTotalAtom = atom<number>(0);
export const offersLoadingAtom = atom<boolean>(false);

export type OfferModal = null | { mode: 'create' } | { mode: 'edit'; offer: SupplierOfferDTO };
export const offerModalAtom = atom<OfferModal>(null);
export const offerDeletingAtom = atom<SupplierOfferDTO | null>(null);

// ── 按店映射（offer_links）────────────────────────────────────────────────
export interface LinkFilters {
  store_id: string;
  keyword: string;
  page: number;
  page_size: number;
}

export const linkFiltersAtom = atom<LinkFilters>({ store_id: '', keyword: '', page: 1, page_size: 20 });
export const linksAtom = atom<OfferLinkDTO[]>([]);
export const linksTotalAtom = atom<number>(0);
export const linksLoadingAtom = atom<boolean>(false);

export type LinkModal = null | { mode: 'create' } | { mode: 'edit'; link: OfferLinkDTO };
export const linkModalAtom = atom<LinkModal>(null);
export const linkDeletingAtom = atom<OfferLinkDTO | null>(null);

export const catalogSubmittingAtom = atom<boolean>(false);
