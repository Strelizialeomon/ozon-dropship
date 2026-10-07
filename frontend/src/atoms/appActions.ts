// 登录用户的写操作（hi-jotai web profile：useXxxActions 返回纯函数）。
import { useSetAtom } from 'jotai';
import { logout as apiLogout, me as apiMe, type MeDTO } from '@/api/auth';
import { meAtom } from './app';

export function useAppActions() {
  const setMe = useSetAtom(meAtom);

  /** 拉当前用户写进 meAtom；失败（含 401）返回 null，401 由拦截器通知守卫跳登录。 */
  async function loadMe(): Promise<MeDTO | null> {
    try {
      const dto = await apiMe();
      setMe(dto);
      return dto;
    } catch {
      setMe(null);
      return null;
    }
  }

  /** 登出：先尽力通知后端，再清本地状态（接口失败不卡住人）。 */
  async function logout(): Promise<void> {
    try {
      await apiLogout();
    } catch {
      // 拦截器已提示；本地照常清干净
    } finally {
      setMe(null);
    }
  }

  return { loadMe, logout };
}
