// 共享 atom（meAtom / isAdminAtom）+ appActions 的真实链路测试：
// 走真实 axios 实例与拦截器，只把 adapter 换成假响应。

import { afterEach, describe, expect, it } from 'bun:test';
import { act, render, screen, waitFor } from '@testing-library/react';
import { useAtomValue } from 'jotai';
import { useAppActions } from './appActions';
import { isAdminAtom, meAtom } from './app';
import { installMockAdapter, type MockRoutes } from '@/test/mock-adapter';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

function Probe() {
  const me = useAtomValue(meAtom);
  const isAdmin = useAtomValue(isAdminAtom);
  const { loadMe } = useAppActions();
  return (
    <div>
      <span data-testid='name'>{me?.name ?? 'none'}</span>
      <span data-testid='admin'>{String(isAdmin)}</span>
      <button data-testid='load' onClick={() => void loadMe()}>加载</button>
    </div>
  );
}

describe('meAtom / isAdminAtom', () => {
  it('loadMe 成功后写入 meAtom，isAdminAtom 派生正确', async () => {
    const routes: MockRoutes = {
      'GET /api/auth/me': {
        body: { code: 0, message: 'ok', data: { id: 'u1', name: '老板', role: 'admin' } },
      },
    };
    uninstall = installMockAdapter(routes);

    render(<Probe />);
    expect(screen.getByTestId('name').textContent).toBe('none');
    expect(screen.getByTestId('admin').textContent).toBe('false');

    await act(async () => {
      screen.getByTestId('load').click();
    });
    await waitFor(() => {
      expect(screen.getByTestId('name').textContent).toBe('老板');
    });
    expect(screen.getByTestId('admin').textContent).toBe('true');
  });

  it('401 时 meAtom 归 null（拦截器通知守卫，页面不持有半截状态）', async () => {
    const routes: MockRoutes = {
      'GET /api/auth/me': {
        status: 401,
        body: { code: 1, message: '未登录或会话已失效', error: '未授权' },
      },
    };
    uninstall = installMockAdapter(routes);

    render(<Probe />);
    await act(async () => {
      screen.getByTestId('load').click();
    });
    await waitFor(() => {
      expect(screen.getByTestId('name').textContent).toBe('none');
    });
  });
});
