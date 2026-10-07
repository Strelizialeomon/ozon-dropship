import { useSetAtom } from 'jotai';
import { getSystemStatus } from '@/api/system';
import { systemLoadingAtom, systemStatusAtom } from './store';

export function useSystemActions() {
  const setStatus = useSetAtom(systemStatusAtom);
  const setLoading = useSetAtom(systemLoadingAtom);

  async function loadStatus(): Promise<void> {
    setLoading(true);
    try {
      setStatus(await getSystemStatus());
    } catch {
      // 拦截器已弹统一提示；保持旧数据不清空
    } finally {
      setLoading(false);
    }
  }

  return { loadStatus };
}
