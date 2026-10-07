// 接口契约回归：对着后端 Go 代码敲定的形状（envelope、参数、动作名）别再漂回去。
// 详见重审报告 PR #18（路一 [严重]）：此前手写契约与 D 实装系统性不符。
import { afterEach, describe, expect, it } from 'bun:test';
import { listCredentials } from './credentials';
import { batchOrders, listOrders } from './orders';
import { installMockAdapter, type MockRequestInfo } from '@/test/mock-adapter';

let uninstall: (() => void) | null = null;
afterEach(() => {
  uninstall?.();
  uninstall = null;
});

describe('接口契约（对照后端 Go 代码）', () => {
  it('订单列表信封是 {total, items}（不是 list）', async () => {
    uninstall = installMockAdapter({
      'GET /api/orders': {
        body: {
          code: 0,
          message: 'ok',
          data: { total: 2, items: [{ id: 'o1', posting_number: 'OZ-1' }, { id: 'o2', posting_number: 'OZ-2' }] },
        },
      },
    });
    const data = await listOrders({ page: 1, page_size: 20 });
    expect(data.total).toBe(2);
    expect(data.items.length).toBe(2);
  });

  it('批量动作名是 plan_purchase，body 带 ids/action', async () => {
    const seen: MockRequestInfo[] = [];
    uninstall = installMockAdapter({
      'POST /api/orders/batch': (cfg) => {
        seen.push(cfg);
        return { body: { code: 0, message: '已处理', data: { succeeded: 2, failed: [] } } };
      },
    });
    const res = await batchOrders(['o1', 'o2'], 'plan_purchase');
    // adapter 看到的 data 是 axios 序列化后的 JSON 串
    expect(JSON.parse(seen[0].data as string)).toEqual({
      ids: ['o1', 'o2'],
      action: 'plan_purchase',
      relay_point_id: '',
    });
    expect(res.succeeded).toBe(2);
    expect(res.failed).toEqual([]);
  });

  it('凭据列表：undefined = 不带参数（全部）；空串 = 带 store_id=""（企业级）', async () => {
    const params: unknown[] = [];
    uninstall = installMockAdapter({
      'GET /api/credentials': (cfg) => {
        params.push(cfg.params);
        return { body: { code: 0, message: 'ok', data: [] } };
      },
    });
    await listCredentials(undefined);
    await listCredentials('');
    await listCredentials('s1');
    expect(params[0]).toBeUndefined();
    expect(params[1]).toEqual({ store_id: '' });
    expect(params[2]).toEqual({ store_id: 's1' });
  });
});
