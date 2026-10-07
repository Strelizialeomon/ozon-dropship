import { getDefaultStore, useSetAtom } from 'jotai';
import {
  createOfferLink as apiCreateLink,
  createSupplierOffer as apiCreateOffer,
  deleteOfferLink as apiDeleteLink,
  deleteSupplierOffer as apiDeleteOffer,
  listOfferLinks,
  listSupplierOffers,
  type OfferLinkReq,
  type SupplierOfferReq,
  updateOfferLink as apiUpdateLink,
  updateSupplierOffer as apiUpdateOffer,
} from '@/api/catalog';
import {
  catalogSubmittingAtom,
  type LinkFilters,
  linkFiltersAtom,
  linksAtom,
  linksLoadingAtom,
  type OfferFilters,
  offerFiltersAtom,
  offersAtom,
  offersLoadingAtom,
} from './store';

let offerSeq = 0;
let linkSeq = 0;

export function useOfferActions() {
  const setList = useSetAtom(offersAtom);
  const setLoading = useSetAtom(offersLoadingAtom);
  const setFilters = useSetAtom(offerFiltersAtom);
  const setSubmitting = useSetAtom(catalogSubmittingAtom);

  /** 货源列表不分页（后端起止）：一次拿全，筛选条件透传。 */
  async function loadOffers(q: OfferFilters): Promise<void> {
    const id = ++offerSeq;
    setLoading(true);
    try {
      const rows = await listSupplierOffers(q);
      if (id !== offerSeq) return;
      setList(rows);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === offerSeq) setLoading(false);
    }
  }

  /** 用「此刻」的筛选条件重拉（增删改后刷新用）。 */
  async function reloadOffers(): Promise<void> {
    await loadOffers(getDefaultStore().get(offerFiltersAtom));
  }

  function applyFilters(patch: Partial<OfferFilters>): void {
    let next!: OfferFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch };
      return next;
    });
    void loadOffers(next);
  }

  async function createOffer(req: SupplierOfferReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiCreateOffer(req);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function updateOffer(id: string, req: SupplierOfferReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiUpdateOffer(id, req);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function deleteOffer(id: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiDeleteOffer(id);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  return { loadOffers, reloadOffers, applyFilters, createOffer, updateOffer, deleteOffer };
}

export function useLinkActions() {
  const setList = useSetAtom(linksAtom);
  const setLoading = useSetAtom(linksLoadingAtom);
  const setFilters = useSetAtom(linkFiltersAtom);
  const setSubmitting = useSetAtom(catalogSubmittingAtom);

  async function loadLinks(q: LinkFilters): Promise<void> {
    const id = ++linkSeq;
    setLoading(true);
    try {
      const rows = await listOfferLinks(q);
      if (id !== linkSeq) return;
      setList(rows);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === linkSeq) setLoading(false);
    }
  }

  async function reloadLinks(): Promise<void> {
    await loadLinks(getDefaultStore().get(linkFiltersAtom));
  }

  function applyFilters(patch: Partial<LinkFilters>): void {
    let next!: LinkFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch };
      return next;
    });
    void loadLinks(next);
  }

  async function createLink(req: OfferLinkReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiCreateLink(req);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  /** 后端 PUT 只收 priority / target_stock。 */
  async function updateLink(id: string, body: { priority: number; target_stock: number }): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiUpdateLink(id, body);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function deleteLink(id: string): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiDeleteLink(id);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  return { loadLinks, reloadLinks, applyFilters, createLink, updateLink, deleteLink };
}
