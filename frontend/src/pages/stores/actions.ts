import { useSetAtom } from 'jotai';
import { type CredentialReq, listCredentials, putCredential } from '@/api/credentials';
import {
  createStore as apiCreateStore,
  deleteStore as apiDeleteStore,
  listStores as apiListStores,
  type StoreReq,
  updateStore as apiUpdateStore,
} from '@/api/stores';
import { credentialsAtom, credentialsLoadingAtom, storesAtom, storesLoadingAtom } from './store';

export function useStoreActions() {
  const setStores = useSetAtom(storesAtom);
  const setLoading = useSetAtom(storesLoadingAtom);

  async function loadStores(): Promise<void> {
    setLoading(true);
    try {
      setStores(await apiListStores());
    } catch {
      // 拦截器已提示
    } finally {
      setLoading(false);
    }
  }

  async function createStore(req: StoreReq): Promise<boolean> {
    try {
      await apiCreateStore(req);
      await loadStores();
      return true;
    } catch {
      return false;
    }
  }

  async function updateStore(id: string, req: StoreReq): Promise<boolean> {
    try {
      await apiUpdateStore(id, req);
      await loadStores();
      return true;
    } catch {
      return false;
    }
  }

  async function removeStore(id: string): Promise<boolean> {
    try {
      await apiDeleteStore(id);
      await loadStores();
      return true;
    } catch {
      return false;
    }
  }

  return { loadStores, createStore, updateStore, removeStore };
}

export function useCredentialActions() {
  const setCredentials = useSetAtom(credentialsAtom);
  const setLoading = useSetAtom(credentialsLoadingAtom);

  async function loadCredentials(storeId?: string): Promise<void> {
    setLoading(true);
    try {
      setCredentials(await listCredentials(storeId));
    } catch {
      // 拦截器已提示
    } finally {
      setLoading(false);
    }
  }

  async function saveCredential(req: CredentialReq): Promise<boolean> {
    try {
      await putCredential(req);
      return true;
    } catch {
      return false;
    }
  }

  return { loadCredentials, saveCredential };
}
