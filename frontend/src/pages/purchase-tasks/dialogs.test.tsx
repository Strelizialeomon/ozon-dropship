// 弹窗回归（重审路二 [严重] #1：取消后表单残留会把上一条的金额/单号写进下一条）。
import { afterEach, describe, expect, it } from 'bun:test';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { getDefaultStore } from 'jotai';
import type { PurchaseTaskDTO } from '@/api/purchase-tasks';
import { installMockAdapter, type MockRequestInfo } from '@/test/mock-adapter';
import { BackfillDialog } from './components/BackfillDialog';
import { MarkPaidDialog } from './components/MarkPaidDialog';
import { taskBackfillTargetAtom, taskMarkPaidTargetAtom } from './store';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

const task = (id: string): PurchaseTaskDTO => ({
  id,
  order_id: 'o1',
  supplier_offer_id: null,
  channel: 'self_use',
  executor_type: 'auto',
  status: 'ordered',
  payload: {
    store_id: 's1',
    posting_number: `OZ-${id}`,
    relay_point_id: 'r1',
    relay_name: '仓',
    relay_address: '地址',
    relay_contact: '人',
    ship_deadline: null,
    currency: 'CNY',
    items: [],
  },
  assignee: null,
  deadline: null,
  idempotency_key: null,
  created_at: '2026-10-07T00:00:00Z',
  updated_at: '2026-10-07T00:00:00Z',
});

describe('采购任务弹窗', () => {
  it('记已付款：取消后换一单打开，输入框是空的（不带上一单的金额）', async () => {
    uninstall = installMockAdapter({});
    const store = getDefaultStore();
    const user = userEvent.setup();

    render(<MarkPaidDialog />);

    // 打开 A、填金额、取消
    await act(async () => store.set(taskMarkPaidTargetAtom, task('a')));
    await user.type(screen.getByLabelText('实付金额'), '128.50');
    await user.click(screen.getByRole('button', { name: '取消' }));
    await act(async () => store.set(taskMarkPaidTargetAtom, task('b')));

    const input = screen.getByLabelText('实付金额') as HTMLInputElement;
    expect(input.value).toBe('');
  });

  it('回填：快递号格式不对时给行内错误且不发请求', async () => {
    const posts: MockRequestInfo[] = [];
    uninstall = installMockAdapter({
      'POST /api/purchase-tasks/b/fill-back': (cfg) => {
        posts.push(cfg);
        return { body: { code: 0, message: 'ok', data: task('b') } };
      },
    });
    const store = getDefaultStore();
    const user = userEvent.setup();

    render(<BackfillDialog />);
    await act(async () => store.set(taskBackfillTargetAtom, task('b')));

    await user.type(screen.getByLabelText('国内快递号'), 'x'); // 太短
    await user.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => {
      expect(screen.queryByText(/国内快递号格式不对/)).toBeTruthy();
    });
    expect(posts.length).toBe(0);
  });

  it('回填：合法输入发 POST，body 只带填了的字段', async () => {
    const posts: MockRequestInfo[] = [];
    uninstall = installMockAdapter({
      'POST /api/purchase-tasks/b/fill-back': (cfg) => {
        posts.push(cfg);
        return { body: { code: 0, message: '已回填', data: task('b') } };
      },
    });
    const store = getDefaultStore();
    const user = userEvent.setup();

    render(<BackfillDialog />);
    await act(async () => store.set(taskBackfillTargetAtom, task('b')));

    await user.type(screen.getByLabelText('国内快递号'), 'SF1234567890');
    await user.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => {
      expect(posts.length).toBe(1);
    });
    const body = JSON.parse(posts[0].data as string) as Record<string, unknown>;
    expect(body.domestic_tracking_no).toBe('SF1234567890');
    expect(body.platform_order_id).toBeUndefined();
    expect(body.amount).toBeUndefined();
  });
});
