// 守卫行为（子 spec 验收：未登录或会话失效跳登录页）。
import { afterEach, describe, expect, it } from 'bun:test';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { installMockAdapter, type MockRoutes } from '@/test/mock-adapter';
import { RequireAuth } from './RequireAuth';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

function renderGuarded(initialPath = '/orders') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path='/login' element={<div>登录页占位</div>} />
        <Route element={<RequireAuth />}>
          <Route path='/orders' element={<div>订单页占位</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('RequireAuth', () => {
  it('未登录（/api/auth/me 401）→ 跳登录页', async () => {
    const routes: MockRoutes = {
      'GET /api/auth/me': { status: 401, body: { code: 1, message: '未登录或会话已失效' } },
    };
    uninstall = installMockAdapter(routes);

    renderGuarded();
    await waitFor(() => {
      expect(screen.queryByText('登录页占位')).toBeTruthy();
    });
    expect(screen.queryByText('订单页占位')).toBeNull();
  });

  it('已登录（me 200）→ 放行受保护页面', async () => {
    const routes: MockRoutes = {
      'GET /api/auth/me': { body: { code: 0, message: 'ok', data: { id: 'u1', name: '小王', role: 'operator' } } },
    };
    uninstall = installMockAdapter(routes);

    renderGuarded();
    await waitFor(() => {
      expect(screen.queryByText('订单页占位')).toBeTruthy();
    });
    expect(screen.queryByText('登录页占位')).toBeNull();
  });

  it('会话中途失效（任意请求 401）→ 守卫把人送回登录页', async () => {
    // 先给一个成功的 me，再模拟「浏览中会话过期」
    const routes: MockRoutes = {
      'GET /api/auth/me': { body: { code: 0, message: 'ok', data: { id: 'u1', name: '小王', role: 'operator' } } },
      'GET /api/orders': { status: 401, body: { code: 1, message: '未登录或会话已失效' } },
    };
    uninstall = installMockAdapter(routes);

    renderGuarded();
    await waitFor(() => {
      expect(screen.queryByText('订单页占位')).toBeTruthy();
    });

    // 模拟页面发了个请求、拿到 401：拦截器通知守卫
    const { request } = await import('@/api/client');
    await act(async () => {
      await request({ method: 'GET', url: '/api/orders' }).catch(() => {});
    });
    await waitFor(() => {
      expect(screen.queryByText('登录页占位')).toBeTruthy();
    });
  });
});
