// 请求层：唯一的 axios 实例 + 响应壳解包 + 统一错误策略（子 spec §6 自定细节：
// 401 跳登录，其余错误弹统一提示）。
//
// 后端响应壳（backend/internal/infra/utils/response.go）：
//   { code, message, error?, data? }；code=0 成功、非 0 业务失败（HTTP 仍 200）；
//   只有 401（未登录）/403（角色不够）/429（限流）/5xx（真崩了）用非 200 状态码。
//
// 登录态是 scs 会话 cookie（同源自动带），前端不存 token。

import axios, { type AxiosError, type AxiosRequestConfig } from 'axios';
import { toast } from 'sonner';

export interface RespShell<T = unknown> {
  code: number;
  message: string;
  error?: string;
  data?: T;
}

/** 业务错误：携带后端错误码（1001 校验 / 1002 冲突 / 1004 不存在 / 2001 账密错 / 2002 保险箱未启用…）。 */
export class ApiError extends Error {
  code: number;
  data?: unknown;
  constructor(code: number, message: string, data?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.data = data;
  }
}

const REQUEST_TIMEOUT_MS = 15_000;
const LOGIN_PATH = '/api/auth/login';

export const http = axios.create({
  baseURL: '/',
  timeout: REQUEST_TIMEOUT_MS,
});

// ── 会话失效（401）通知 ───────────────────────────────────────────────────
// 拦截器活在 React 之外，拿不到 navigate；由路由守卫注册回调，跳转交给 React 侧做。
type UnauthorizedListener = () => void;
let unauthorizedListener: UnauthorizedListener | null = null;

/** 由路由守卫注册；传 null 注销。 */
export function setUnauthorizedListener(fn: UnauthorizedListener | null): void {
  unauthorizedListener = fn;
}

http.interceptors.response.use(
  (resp) => {
    const shell = resp.data as RespShell | undefined;
    if (shell && typeof shell.code === 'number') {
      if (shell.code === 0) {
        resp.data = shell.data; // 解包：调用方拿到的就是业务 data
        return resp;
      }
      // HTTP 200 但业务失败
      const err = new ApiError(shell.code, shell.error || shell.message || '请求失败', shell.data);
      if (resp.config.url !== LOGIN_PATH) toast.error(err.message);
      throw err;
    }
    return resp;
  },
  (error: AxiosError) => {
    const status = error.response?.status;
    const shell = error.response?.data as RespShell | undefined;
    if (status === 401) {
      // 会话失效：不弹提示，守卫收到通知后把人送回登录页。
      unauthorizedListener?.();
      throw new ApiError(401, shell?.message || '未登录或会话已失效');
    }
    const msg = shell?.error || shell?.message || error.message || '网络异常，请稍后重试';
    const apiErr = new ApiError(shell?.code ?? status ?? 1, msg, shell?.data);
    // 登录页自己显示行内错误，不弹全局提示。
    if (error.config?.url !== LOGIN_PATH) toast.error(msg);
    throw apiErr;
  },
);

/**
 * 带上响应壳的语义发请求：拦截器已解包，返回的就是业务 data。
 * api 层纯请求函数一律走它，别直接用 http.get。
 */
export async function request<T>(config: AxiosRequestConfig): Promise<T> {
  const resp = await http.request<T>(config);
  return resp.data;
}
