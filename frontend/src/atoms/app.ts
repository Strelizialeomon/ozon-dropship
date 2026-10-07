// 跨 feature 共享 atom：当前登录用户（页面壳、菜单、各页面都读）。
// 按 hi-jotai 房屋风格：atom 只在 store 定义（跨 feature 的放 src/atoms/），写操作走 appActions。

import { atom } from 'jotai';
import type { MeDTO } from '@/api/auth';

/** 当前登录用户；null = 未登录 / 会话已失效。 */
export const meAtom = atom<MeDTO | null>(null);

/** 当前用户是否 admin（菜单可见性、admin 页面守卫用）。 */
export const isAdminAtom = atom((get) => get(meAtom)?.role === 'admin');
