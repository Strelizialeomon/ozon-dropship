import { useAtom, useAtomValue } from 'jotai';
import { Search, ShoppingCart, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
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
import { useOrderBatchActions, useOrderDetailActions, useOrderListActions } from './actions';
import {
  orderBatchSubmittingAtom,
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
  const batchSubmitting = useAtomValue(orderBatchSubmittingAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);
  const [keywordInput, setKeywordInput] = useState(filters.keyword);

  const { loadOrders, reloadOrders, applyFilters, changePage, resetFilters } = useOrderListActions();
  const { planPurchase } = useOrderBatchActions();
  const { loadOrderDetail } = useOrderDetailActions();
  const { ensureStores } = useStoreOptionsActions();

  // 列表行只带 store_id：用店铺下拉的共享数据把店名映射出来
  const storeName = useMemo(() => new Map(storeOptions.map((s) => [s.id, s.name])), [storeOptions]);

  useEffect(() => {
    void ensureStores();
    void loadOrders(filters);
    // 仅首挂载：后续筛选/翻页走 actions
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selectedIds = Object.keys(selection);

  async function onBatchPlanPurchase(): Promise<void> {
    const result = await planPurchase(selectedIds);
    if (!result) return;
    if (result.failed.length === 0) {
      toast.success(`已生成 ${result.succeeded} 个采购任务`);
    } else {
      toast.warning(`成功 ${result.succeeded} 单、失败 ${result.failed.length} 单`, {
        description: result.failed
          .slice(0, 3)
          .map((f) => `${f.id}: ${f.error}`)
          .join('\n'),
      });
    }
    setSelection({});
    void reloadOrders();
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

        <Button
          variant='ghost'
          size='sm'
          onClick={() => {
            setKeywordInput('');
            resetFilters();
          }}
        >
          重置
        </Button>

        {selectedIds.length > 0 && (
          <div className='ml-auto flex items-center gap-2 rounded-md border bg-background px-3 py-1.5'>
            <span className='text-sm'>已选 {selectedIds.length} 单</span>
            <Button size='sm' disabled={batchSubmitting} onClick={() => void onBatchPlanPurchase()}>
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
        onRowClick={(o) => void loadOrderDetail(o.id)}
        pagination={pagination}
        emptyText='没有符合条件的订单'
        columns={[
          selectionColumn<OrderDTO>(),
          {
            id: 'posting_number',
            header: 'posting',
            accessorFn: (o) => o.posting_number,
            cell: ({ row }) => <span className='font-mono text-xs'>{row.original.posting_number}</span>,
          },
          {
            id: 'store',
            header: '店铺',
            accessorFn: (o) => storeName.get(o.store_id) ?? o.store_id,
            cell: ({ row }) => storeName.get(row.original.store_id) ?? row.original.store_id,
          },
          {
            id: 'status',
            header: '状态',
            accessorFn: (o) => o.status,
            cell: ({ row }) => <StatusBadge spec={orderStatus(row.original.status)} />,
          },
          {
            id: 'ozon_status',
            header: 'Ozon 状态',
            accessorFn: (o) => o.ozon_status,
            cell: ({ row }) => <span className='text-xs text-muted-foreground'>{row.original.ozon_status}</span>,
          },
          {
            id: 'ship_deadline',
            header: '发货截止',
            accessorFn: (o) => o.ship_deadline ?? '',
            cell: ({ row }) => {
              const d = row.original.ship_deadline;
              const urgent = d && new Date(d).getTime() - Date.now() < 24 * 3600_000;
              return <span className={urgent ? 'text-amber-700' : undefined}>{formatDateTime(d)}</span>;
            },
          },
          {
            id: 'amount',
            header: '金额',
            accessorFn: (o) => Number(o.total_amount),
            cell: ({ row }) => formatMoney(row.original.total_amount, row.original.currency),
          },
          {
            id: 'tpl',
            header: '物流方式',
            accessorFn: (o) => o.tpl_integration_type ?? '',
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
