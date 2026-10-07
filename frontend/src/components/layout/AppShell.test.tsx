// 菜单角色可见性（子 spec 验收：operator 看不到 admin 菜单）。
import { afterEach, describe, expect, it } from 'bun:test';
import { render, screen } from '@testing-library/react';
import { getDefaultStore } from 'jotai';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { meAtom } from '@/atoms/app';
import { installMockAdapter } from '@/test/mock-adapter';
import { AppShell } from './AppShell';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

function renderShell(role: 'admin' | 'operator') {
  const store = getDefaultStore();
  store.set(meAtom, { id: 'u1', name: '测试', role });
  uninstall = installMockAdapter({});
  return render(
    <MemoryRouter initialEntries={['/orders']}>
      <Routes>
        <Route element={<AppShell />}>
          <Route path='/orders' element={<div>订单内容</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('AppShell 菜单', () => {
  it('operator 看不到「队列监控」入口', () => {
    renderShell('operator');
    expect(screen.queryByText('订单工作台')).toBeTruthy();
    expect(screen.queryByText('队列监控')).toBeNull();
  });

  it('admin 能看到「队列监控」入口（指向后端 asynqmon）', () => {
    renderShell('admin');
    const link = screen.queryByText('队列监控');
    expect(link).toBeTruthy();
    expect(link?.closest('a')?.getAttribute('href')).toBe('/api/admin/queues');
  });
});
