// 登录页：账密错行内报错（code 2001）、成功跳转。
import { afterEach, describe, expect, it } from 'bun:test';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { installMockAdapter, type MockRoutes } from '@/test/mock-adapter';
import LoginPage from './index';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <Routes>
        <Route path='/login' element={<LoginPage />} />
        <Route path='/' element={<div>进入工作台</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('登录页', () => {
  it('账密错：HTTP 200 + code 2001 → 行内显示后端消息', async () => {
    const routes: MockRoutes = {
      'POST /api/auth/login': { body: { code: 2001, message: '用户名或密码错误' } },
    };
    uninstall = installMockAdapter(routes);
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('用户名'), 'someone');
    await user.type(screen.getByLabelText('密码'), 'bad-pass');
    await user.click(screen.getByRole('button', { name: '登录' }));

    await waitFor(() => {
      expect(screen.queryByText('用户名或密码错误')).toBeTruthy();
    });
    expect(screen.queryByText('进入工作台')).toBeNull();
  });

  it('登录成功 → 跳到工作台', async () => {
    const routes: MockRoutes = {
      'POST /api/auth/login': {
        body: { code: 0, message: 'ok', data: { id: 'u1', name: '小王', role: 'operator' } },
      },
    };
    uninstall = installMockAdapter(routes);
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('用户名'), 'wang');
    await user.type(screen.getByLabelText('密码'), 'secret');
    await user.click(screen.getByRole('button', { name: '登录' }));

    await waitFor(() => {
      expect(screen.queryByText('进入工作台')).toBeTruthy();
    });
  });
});
