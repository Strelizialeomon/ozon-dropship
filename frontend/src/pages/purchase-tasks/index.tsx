import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { Search } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import type { PurchaseTaskDTO } from '@/api/purchase-tasks';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { formatDateTime, formatMoney } from '@/lib/format';
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

const CHANNEL_OPTIONS = [
  { value: 'self_use', label: '1688 自用版' },
  { value: 'cross_border', label: '1688 跨境版' },
  { value: 'manual', label: '人工渠道' },
];

const EXECUTOR_OPTIONS = [
  { value: 'auto', label: '自动' },
  { value: 'manual', label: '人工' },
];

/** 行上该显示哪些动作（按执行器 × 状态推导，照总纲 §5.1 的状态机）。 */
function rowActions(t: PurchaseTaskDTO) {
  const busy = ['closed'].includes(t.status);
  return {
    execute: t.executor_type === 'auto' && (t.status === 'pending' || t.status === 'exception'),
    toManual: t.executor_type === 'auto' && (t.status === 'pending' || t.status === 'exception'),
    markPaid: t.status === 'ordered',
    prepSheet: t.executor_type === 'manual' && !busy && t.status !== 'paid' && t.status !== 'shipped',
    backfill: t.status === 'paid' || t.status === 'shipped' || (t.executor_type === 'manual' && t.status === 'ordered'),
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
  const [keywordInput, setKeywordInput] = useState(filters.keyword);

  const { loadTasks, applyFilters, changePage, resetFilters } = useTaskListActions();
  const { execute, toManual, openPrepSheet } = useTaskActions();

  useEffect(() => {
    void loadTasks(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onExecute(): Promise<void> {
    if (!executeTarget) return;
    const ok = await execute(executeTarget.id);
    if (ok) {
      toast.success('已发起自动下单');
      void loadTasks(filters);
    }
    setExecuteTarget(null);
  }

  async function onToManual(): Promise<void> {
    if (!toManualTarget) return;
    const ok = await toManual(toManualTarget.id);
    if (ok) {
      toast.success('已转人工');
      void loadTasks(filters);
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
          value={filters.channel || '_all'}
          onValueChange={(v) => applyFilters({ channel: v === '_all' ? '' : v })}
        >
          <SelectTrigger className='w-36'>
            <SelectValue placeholder='全部渠道' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部渠道</SelectItem>
            {CHANNEL_OPTIONS.map((o) => (
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

        <div className='flex items-center gap-1'>
          <Input
            className='w-52'
            placeholder='posting / 店铺'
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
            cell: ({ row }) => <span className='font-mono text-xs'>{row.original.posting_number}</span>,
          },
          { id: 'store', header: '店铺', cell: ({ row }) => row.original.store_name },
          {
            id: 'channel',
            header: '渠道',
            cell: ({ row }) => <StatusBadge spec={channelLabel(row.original.channel)} />,
          },
          {
            id: 'executor',
            header: '执行器',
            cell: ({ row }) => EXECUTOR[row.original.executor_type] ?? row.original.executor_type,
          },
          { id: 'status', header: '状态', cell: ({ row }) => <StatusBadge spec={taskStatus(row.original.status)} /> },
          { id: 'assignee', header: '负责人', cell: ({ row }) => row.original.assignee || '—' },
          { id: 'deadline', header: '截止', cell: ({ row }) => formatDateTime(row.original.deadline) },
          {
            id: 'paid',
            header: '实付 / 单号',
            cell: ({ row }) => {
              const po = row.original.purchase_order;
              if (!po) return '—';
              return (
                <div className='text-xs'>
                  <div>{formatMoney(po.amount, po.currency ?? 'CNY')}</div>
                  <div className='text-muted-foreground'>{po.platform_order_id || '—'}</div>
                </div>
              );
            },
          },
          { id: 'created', header: '创建时间', cell: ({ row }) => formatDateTime(row.original.created_at) },
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
          executeTarget?.posting_number ?? ''
        } 调 1688 下单接口（S1 无免密支付，下单后停在「已下单」，需人工付款后记付款）。`}
        confirmText='下单'
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
