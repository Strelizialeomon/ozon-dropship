import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { Search, ShoppingCart, X } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import type { OrderDTO } from '@/api/orders';
import { storeOptionsAtom, useStoreOptionsActions } from '@/atoms/storeOptions';
import { DataTable, selectionColumn, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { formatDateTime, formatMoney } from '@/lib/format';
import { ORDER_STATUS_OPTIONS, orderStatus, tplIntegration } from '@/lib/labels';
import { OrderDetailDialog } from './components/OrderDetailDialog';
import { useOrderBatchActions, useOrderListActions } from './actions';
import {
  orderBatchSubmittingAtom,
  orderDetailAtom,
  orderFiltersAtom,
  ordersAtom,
  orderSelectionAtom,
  ordersLoadingAtom,
  ordersTotalAtom,
} from './store';

export default function OrdersPage() {
  const orders = useAtomValue(ordersAtom);
  const total = useAtomValue(ordersTotalAtom);
  const loading = useAtomValue(ordersLoadingAtom);
  const filters = useAtomValue(orderFiltersAtom);
  const [selection, setSelection] = useAtom(orderSelectionAtom);
  const setDetail = useSetAtom(orderDetailAtom);
  const batchSubmitting = useAtomValue(orderBatchSubmittingAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);
  const [keywordInput, setKeywordInput] = useState(filters.keyword);

  const { loadOrders, applyFilters, changePage, resetFilters } = useOrderListActions();
  const { createPurchaseTasks } = useOrderBatchActions();
  const { ensureStores } = useStoreOptionsActions();

  useEffect(() => {
    void ensureStores();
    void loadOrders(filters);
    // 仅首挂载：后续筛选/翻页走 actions
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selectedIds = Object.keys(selection);

  async function onBatchCreateTasks(): Promise<void> {
    const result = await createPurchaseTasks(selectedIds);
    if (!result) return;
    toast.success(`已生成 ${result.acted} 个采购任务${result.skipped > 0 ? `，跳过 ${result.skipped} 单` : ''}`);
    setSelection({});
    void loadOrders(filters);
  }

  const pagination: ServerPagination = {
    page: filters.page,
    pageSize: filters.page_size,
    total,
    onPageChange: (p) => changePage(p),
  };

  return (
    <div>
      <PageHeader title='订单工作台' description='Ozon 拉回的 posting 全量视图；勾选后可批量生成采购任务' />

      <div className='mb-3 flex flex-wrap items-center gap-2'>
        <Select
          value={filters.store_id || '_all'}
          onValueChange={(v) => applyFilters({ store_id: v === '_all' ? '' : v })}
        >
          <SelectTrigger className='w-44'>
            <SelectValue placeholder='全部店铺' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部店铺</SelectItem>
            {storeOptions.map((s) => (
              <SelectItem key={s.id} value={s.id}>
                {s.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={filters.status || '_all'} onValueChange={(v) => applyFilters({ status: v === '_all' ? '' : v })}>
          <SelectTrigger className='w-40'>
            <SelectValue placeholder='全部状态' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部状态</SelectItem>
            {ORDER_STATUS_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className='flex items-center gap-1'>
          <Input
            className='w-56'
            placeholder='posting / 订单号'
            value={keywordInput}
            onChange={(e) => setKeywordInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && applyFilters({ keyword: keywordInput })}
          />
          <Button variant='outline' size='icon' onClick={() => applyFilters({ keyword: keywordInput })}>
            <Search className='size-4' />
          </Button>
        </div>

        <Button variant='ghost' size='sm' onClick={resetFilters}>
          重置
        </Button>

        {selectedIds.length > 0 && (
          <div className='ml-auto flex items-center gap-2 rounded-md border bg-background px-3 py-1.5'>
            <span className='text-sm'>已选 {selectedIds.length} 单</span>
            <Button size='sm' disabled={batchSubmitting} onClick={() => void onBatchCreateTasks()}>
              <ShoppingCart className='size-4' />
              生成采购任务
            </Button>
            <Button variant='ghost' size='icon' className='size-7' onClick={() => setSelection({})}>
              <X className='size-4' />
            </Button>
          </div>
        )}
      </div>

      <DataTable
        data={orders}
        loading={loading}
        getRowId={(o) => o.id}
        rowSelection={selection}
        onRowSelectionChange={setSelection}
        onRowClick={(o) => setDetail(o)}
        pagination={pagination}
        emptyText='没有符合条件的订单'
        columns={[
          selectionColumn<OrderDTO>(),
          {
            id: 'posting_number',
            header: 'posting',
            cell: ({ row }) => <span className='font-mono text-xs'>{row.original.posting_number}</span>,
          },
          { id: 'store', header: '店铺', cell: ({ row }) => row.original.store_name },
          { id: 'status', header: '状态', cell: ({ row }) => <StatusBadge spec={orderStatus(row.original.status)} /> },
          {
            id: 'ozon_status',
            header: 'Ozon 状态',
            cell: ({ row }) => <span className='text-xs text-muted-foreground'>{row.original.ozon_status}</span>,
          },
          {
            id: 'ship_deadline',
            header: '发货截止',
            cell: ({ row }) => {
              const d = row.original.ship_deadline;
              const urgent = d && new Date(d).getTime() - Date.now() < 24 * 3600_000;
              return <span className={urgent ? 'text-amber-700' : undefined}>{formatDateTime(d)}</span>;
            },
          },
          {
            id: 'amount',
            header: '金额',
            cell: ({ row }) => formatMoney(row.original.total_amount, row.original.currency),
          },
          { id: 'items', header: '商品数', cell: ({ row }) => row.original.items.length },
          {
            id: 'tpl',
            header: '物流方式',
            cell: ({ row }) => (
              <span className='text-xs text-muted-foreground'>{tplIntegration(row.original.tpl_integration_type)}</span>
            ),
          },
        ]}
      />

      <OrderDetailDialog />
    </div>
  );
}
