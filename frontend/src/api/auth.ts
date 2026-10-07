// 登录 / 登出 / 当前用户（backend/internal/auth/auth.go）。
import { request } from './client';

export type Role = 'admin' | 'operator';

export interface MeDTO {
  id: string;
  name: string;
  role: Role;
}

/** POST /api/auth/login —— 账密错走 code=2001，超限走 HTTP 429（登录页自行显示）。 */
export function login(name: string, password: string): Promise<MeDTO> {
  return request<MeDTO>({ method: 'POST', url: '/api/auth/login', data: { name, password } });
}

/** POST /api/auth/logout */
export function logout(): Promise<void> {
  return request<void>({ method: 'POST', url: '/api/auth/logout' });
}

/** GET /api/auth/me —— 会话无效时 401（拦截器会通知守卫）。 */
export function me(): Promise<MeDTO> {
  return request<MeDTO>({ method: 'GET', url: '/api/auth/me' });
}
