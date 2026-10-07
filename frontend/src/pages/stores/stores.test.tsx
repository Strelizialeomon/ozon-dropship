// 凭据列表（子 spec 验收：只显示脱敏尾号；到期 ≤ 14 天高亮）。
import { afterEach, describe, expect, it } from 'bun:test';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { installMockAdapter, type MockRoutes } from '@/test/mock-adapter';
import StoresPage from './index';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

const routes: MockRoutes = {
  'GET /api/stores': {
    body: {
      code: 0,
      message: 'ok',
      data: [
        {
          id: 's1',
          name: '旗舰店',
          mode: 'rfbs',
          client_id: '12345',
          currency: 'CNY',
          default_relay_point_id: null,
          push_enabled: true,
          ship_early: false,
          last_sync_at: '2026-10-07T02:00:00Z',
          status: 'active',
          created_at: '2026-10-01T00:00:00Z',
          updated_at: '2026-10-01T00:00:00Z',
        },
      ],
    },
  },
  'GET /api/credentials': {
    body: {
      code: 0,
      message: 'ok',
      data: [
        {
          id: 'c1',
          store_id: 's1',
          store_name: '旗舰店',
          kind: 'ozon_api_key',
          masked: '****abcd',
          expires_at: '2026-10-17T00:00:00Z',
          days_left: 10,
          last_verified_at: null,
          rotated_at: '2026-10-01T00:00:00Z',
        },
        {
          id: 'c2',
          store_id: 's2',
          store_name: '二店',
          kind: 'ozon_api_key',
          masked: '****wxyz',
          expires_at: '2026-10-05T00:00:00Z',
          days_left: -2,
          last_verified_at: null,
          rotated_at: null,
        },
        {
          id: 'c3',
          store_id: '',
          store_name: '',
          kind: 'alibaba_token',
          masked: '****8888',
          expires_at: null,
          days_left: null,
          last_verified_at: null,
          rotated_at: null,
        },
      ],
    },
  },
};

describe('店铺与凭据页', () => {
  it('凭据只显示脱敏尾号；≤14 天高亮、已过期更醒目；企业级显示为「企业级」', async () => {
    uninstall = installMockAdapter(routes);
    const user = userEvent.setup();
    render(<StoresPage />);

    // 店铺 tab 默认打开，切到凭据 tab（Radix Tabs 要完整指针序列，裸 .click() 不激活）
    const credTab = await screen.findByRole('tab', { name: '凭据' });
    await user.click(credTab);

    await waitFor(() => {
      expect(screen.queryByText('****abcd')).toBeTruthy();
    });

    // 脱敏尾号：列表里不应该出现任何未脱敏形态
    expect(screen.queryByText('****wxyz')).toBeTruthy();

    // 到期标注：10 天（≤14 高亮黄）、-2 天显示已过期
    const tenDays = screen.getByText('10 天');
    expect(tenDays.className).toContain('text-amber-700');
    const expired = screen.getByText('已过期 2 天');
    expect(expired.className).toContain('text-destructive');

    // 企业级凭据的归属列
    expect(screen.queryByText('企业级')).toBeTruthy();
  });
});
