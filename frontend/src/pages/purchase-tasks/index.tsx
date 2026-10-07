import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { useEffect, useMemo } from 'react';
import { toast } from 'sonner';
import type { PurchaseTaskDTO } from '@/api/purchase-tasks';
import { storeOptionsAtom, useStoreOptionsActions } from '@/atoms/storeOptions';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { formatDateTime } from '@/lib/format';
import { channelLabel, EXECUTOR, TASK_STATUS_OPTIONS, taskStatus } from '@/lib/labels';
import { BackfillDialog } from './components/BackfillDialog';
import { MarkPaidDialog } from './components/MarkPaidDialog';
import { PrepSheetDialog } from './components/PrepSheetDialog';
import { useTaskActions, useTaskListActions } from './actions';
import {
  taskActionSubmittingAtom,
  taskBackfillTargetAtom,
  taskExecutingTargetAtom,
  taskFiltersAtom,
  taskMarkPaidTargetAtom,
  tasksAtom,
  tasksLoadingAtom,
  tasksTotalAtom,
  taskToManualTargetAtom,
} from './store';

const EXECUTOR_OPTIONS = [
  { value: 'auto', label: '自动' },
  { value: 'manual', label: '人工' },
];

/** 行上该显示哪些动作（对齐后端各动作的状态校验，见 purchase/handler.go、manual.go）。 */
function rowActions(t: PurchaseTaskDTO) {
  const st = t.status;
  return {
    execute: t.executor_type === 'auto' && (st === 'pending' || st === 'exception'),
    toManual: t.executor_type === 'auto' && (st === 'pending' || st === 'exception'),
    markPaid: st === 'ordered' || st === 'exception',
    prepSheet: t.executor_type === 'manual'
      && (st === 'pending' || st === 'executing' || st === 'exception' || st === 'ordered'),
    backfill: st === 'ordered' || st === 'paid' || st === 'exception',
  };
}

export default function PurchaseTasksPage() {
  const tasks = useAtomValue(tasksAtom);
  const total = useAtomValue(tasksTotalAtom);
  const loading = useAtomValue(tasksLoadingAtom);
  const filters = useAtomValue(taskFiltersAtom);
  const submitting = useAtomValue(taskActionSubmittingAtom);
  const [executeTarget, setExecuteTarget] = useAtom(taskExecutingTargetAtom);
  const [toManualTarget, setToManualTarget] = useAtom(taskToManualTargetAtom);
  const setMarkPaidTarget = useSetAtom(taskMarkPaidTargetAtom);
  const setBackfillTarget = useSetAtom(taskBackfillTargetAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);

  const { loadTasks, reloadTasks, applyFilters, changePage, resetFilters } = useTaskListActions();
  const { execute, toManual, openPrepSheet } = useTaskActions();
  const { ensureStores } = useStoreOptionsActions();

  const storeName = useMemo(() => new Map(storeOptions.map((s) => [s.id, s.name])), [storeOptions]);

  useEffect(() => {
    void ensureStores();
    void loadTasks(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onExecute(): Promise<void> {
    if (!executeTarget) return;
    const ok = await execute(executeTarget.id);
    if (ok) {
      toast.success('已投递执行');
      void reloadTasks();
    }
    setExecuteTarget(null);
  }

  async function onToManual(): Promise<void> {
    if (!toManualTarget) return;
    const ok = await toManual(toManualTarget.id);
    if (ok) {
      toast.success('已转人工');
      void reloadTasks();
    }
    setToManualTarget(null);
  }

  const pagination: ServerPagination = {
    page: filters.page,
    pageSize: filters.page_size,
    total,
    onPageChange: (p) => changePage(p),
  };

  return (
    <div>
      <PageHeader
        title='采购任务台'
        description='自动 / 人工两种执行器统一在这处理：执行、转人工、记付款、备料单、回填'
      />

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
          <SelectTrigger className='w-36'>
            <SelectValue placeholder='全部状态' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部状态</SelectItem>
            {TASK_STATUS_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select
          value={filters.executor_type || '_all'}
          onValueChange={(v) => applyFilters({ executor_type: v === '_all' ? '' : v })}
        >
          <SelectTrigger className='w-32'>
            <SelectValue placeholder='执行器' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部执行器</SelectItem>
            {EXECUTOR_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Button variant='ghost' size='sm' onClick={resetFilters}>
          重置
        </Button>
      </div>

      <DataTable
        data={tasks}
        loading={loading}
        getRowId={(t) => t.id}
        pagination={pagination}
        emptyText='没有符合条件的采购任务'
        columns={[
          {
            id: 'posting',
            header: 'posting',
            accessorFn: (t) => t.payload?.posting_number ?? '',
            cell: ({ row }) => <span className='font-mono text-xs'>{row.original.payload?.posting_number ?? '—'}</span>,
          },
          {
            id: 'store',
            header: '店铺',
            accessorFn: (t) => (t.payload ? storeName.get(t.payload.store_id) ?? t.payload.store_id : ''),
            cell: ({ row }) => {
              const sid = row.original.payload?.store_id;
              return sid ? storeName.get(sid) ?? sid : '—';
            },
          },
          {
            id: 'channel',
            header: '渠道',
            accessorFn: (t) => t.channel,
            cell: ({ row }) => <StatusBadge spec={channelLabel(row.original.channel)} />,
          },
          {
            id: 'executor',
            header: '执行器',
            accessorFn: (t) => t.executor_type,
            cell: ({ row }) => EXECUTOR[row.original.executor_type] ?? row.original.executor_type,
          },
          {
            id: 'status',
            header: '状态',
            accessorFn: (t) => t.status,
            cell: ({ row }) => <StatusBadge spec={taskStatus(row.original.status)} />,
          },
          {
            id: 'assignee',
            header: '负责人',
            accessorFn: (t) => t.assignee ?? '',
            cell: ({ row }) => row.original.assignee || '—',
          },
          {
            id: 'deadline',
            header: '截止',
            accessorFn: (t) => t.deadline ?? '',
            cell: ({ row }) => formatDateTime(row.original.deadline),
          },
          {
            id: 'created',
            header: '创建时间',
            accessorFn: (t) => t.created_at,
            cell: ({ row }) => formatDateTime(row.original.created_at),
          },
          {
            id: 'actions',
            header: '操作',
            cell: ({ row }) => {
              const t = row.original;
              const a = rowActions(t);
              return (
                <div className='flex flex-wrap gap-1'>
                  {a.execute && (
                    <Button
                      variant='secondary'
                      size='sm'
                      disabled={submitting}
                      onClick={() => setExecuteTarget(t)}
                    >
                      立即执行
                    </Button>
                  )}
                  {a.toManual && (
                    <Button
                      variant='ghost'
                      size='sm'
                      disabled={submitting}
                      onClick={() => setToManualTarget(t)}
                    >
                      转人工
                    </Button>
                  )}
                  {a.markPaid && (
                    <Button variant='secondary' size='sm' onClick={() => setMarkPaidTarget(t)}>
                      记已付款
                    </Button>
                  )}
                  {a.prepSheet && (
                    <Button variant='ghost' size='sm' onClick={() => void openPrepSheet(t.id)}>
                      备料单
                    </Button>
                  )}
                  {a.backfill && (
                    <Button variant='secondary' size='sm' onClick={() => setBackfillTarget(t)}>
                      回填
                    </Button>
                  )}
                </div>
              );
            },
          },
        ]}
      />

      <ConfirmDialog
        open={!!executeTarget}
        onOpenChange={(open) => !open && setExecuteTarget(null)}
        title='立即执行自动下单？'
        description={`将对 ${
          executeTarget?.payload?.posting_number ?? executeTarget?.id ?? ''
        } 投递自动执行（S1 无免密支付，下单后停在「已下单」，需人工付款后记付款）。`}
        confirmText='执行'
        busy={submitting}
        onConfirm={() => void onExecute()}
      />

      <ConfirmDialog
        open={!!toManualTarget}
        onOpenChange={(open) => !open && setToManualTarget(null)}
        title='转为人工执行？'
        description='任务改出备料单，由人工去平台下单并回填（新商家首单按规则也走人工）。'
        confirmText='转人工'
        busy={submitting}
        onConfirm={() => void onToManual()}
      />

      <MarkPaidDialog />
      <BackfillDialog />
      <PrepSheetDialog />
    </div>
  );
}
