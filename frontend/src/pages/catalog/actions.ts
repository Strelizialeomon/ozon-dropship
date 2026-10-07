import { useSetAtom } from 'jotai';
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
  linksTotalAtom,
  type OfferFilters,
  offerFiltersAtom,
  offersAtom,
  offersLoadingAtom,
  offersTotalAtom,
} from './store';

let offerSeq = 0;
let linkSeq = 0;

export function useOfferActions() {
  const setList = useSetAtom(offersAtom);
  const setTotal = useSetAtom(offersTotalAtom);
  const setLoading = useSetAtom(offersLoadingAtom);
  const setFilters = useSetAtom(offerFiltersAtom);
  const setSubmitting = useSetAtom(catalogSubmittingAtom);

  async function loadOffers(q: OfferFilters): Promise<void> {
    const id = ++offerSeq;
    setLoading(true);
    try {
      const data = await listSupplierOffers(q);
      if (id !== offerSeq) return;
      setList(data.list);
      setTotal(data.total);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === offerSeq) setLoading(false);
    }
  }

  function applyFilters(patch: Partial<OfferFilters>): void {
    let next!: OfferFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadOffers(next);
  }

  function changePage(page: number): void {
    let next!: OfferFilters;
    setFilters((prev) => {
      next = { ...prev, page };
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
    try {
      await apiDeleteOffer(id);
      return true;
    } catch {
      return false;
    }
  }

  return { loadOffers, applyFilters, changePage, createOffer, updateOffer, deleteOffer };
}

export function useLinkActions() {
  const setList = useSetAtom(linksAtom);
  const setTotal = useSetAtom(linksTotalAtom);
  const setLoading = useSetAtom(linksLoadingAtom);
  const setFilters = useSetAtom(linkFiltersAtom);
  const setSubmitting = useSetAtom(catalogSubmittingAtom);

  async function loadLinks(q: LinkFilters): Promise<void> {
    const id = ++linkSeq;
    setLoading(true);
    try {
      const data = await listOfferLinks(q);
      if (id !== linkSeq) return;
      setList(data.list);
      setTotal(data.total);
    } catch {
      // 拦截器已提示
    } finally {
      if (id === linkSeq) setLoading(false);
    }
  }

  function applyFilters(patch: Partial<LinkFilters>): void {
    let next!: LinkFilters;
    setFilters((prev) => {
      next = { ...prev, ...patch, page: 1 };
      return next;
    });
    void loadLinks(next);
  }

  function changePage(page: number): void {
    let next!: LinkFilters;
    setFilters((prev) => {
      next = { ...prev, page };
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

  async function updateLink(id: string, req: OfferLinkReq): Promise<boolean> {
    setSubmitting(true);
    try {
      await apiUpdateLink(id, req);
      return true;
    } catch {
      return false;
    } finally {
      setSubmitting(false);
    }
  }

  async function deleteLink(id: string): Promise<boolean> {
    try {
      await apiDeleteLink(id);
      return true;
    } catch {
      return false;
    }
  }

  return { loadLinks, applyFilters, changePage, createLink, updateLink, deleteLink };
}
