// 测试用 axios adapter：按「方法 + 路径」返回预置响应壳。
// 用它而不是 mock 模块，是为了让测试连真实拦截器（解包 / 401 / 统一提示）一起跑。
//
// ⚠️ 非 2xx 必须自己 reject（axios 的 settle 是各 adapter 内部做的，
// 自定义 adapter 只 resolve 的话 401 会被当成成功响应 —— 当年这个坑让「中途掉线」测试假绿）。
import { type AxiosAdapter, AxiosError, type AxiosResponse } from 'axios';
import { http } from '@/api/client';

export interface MockRoute {
  status?: number;
  body: unknown;
}

export interface MockRequestInfo {
  data?: unknown;
  params?: unknown;
  url?: string;
}

export type MockRoutes = Record<string, MockRoute | ((cfg: MockRequestInfo) => MockRoute)>;

/** 装一个假 adapter，返回卸载函数（afterEach 里调）。 */
export function installMockAdapter(routes: MockRoutes): () => void {
  const adapter: AxiosAdapter = async (config) => {
    const key = `${(config.method ?? 'get').toUpperCase()} ${config.url}`;
    const hit = routes[key];
    let route: MockRoute = { status: 404, body: { code: 1, message: `mock 未定义路由: ${key}` } };
    if (hit) {
      route = typeof hit === 'function'
        ? hit({ data: config.data, params: config.params, url: config.url })
        : hit;
    }
    const resp: AxiosResponse = {
      data: route.body,
      status: route.status ?? 200,
      statusText: '',
      headers: {},
      config,
    };
    const status = resp.status;
    if (status >= 200 && status < 300) return resp;
    throw new AxiosError(
      `Request failed with status code ${status}`,
      status >= 500 ? AxiosError.ERR_BAD_RESPONSE : AxiosError.ERR_BAD_REQUEST,
      config,
      null,
      resp,
    );
  };
  http.defaults.adapter = adapter;
  return () => {
    http.defaults.adapter = undefined;
  };
}
