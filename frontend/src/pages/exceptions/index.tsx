import { useAtom, useAtomValue } from 'jotai';
import { useEffect } from 'react';
import { toast } from 'sonner';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { formatDateTime } from '@/lib/format';
import { EXCEPTION_CODE_OPTIONS, exceptionCode, REF_TYPE } from '@/lib/labels';
import { useExceptionActions } from './actions';
import {
  exceptionFiltersAtom,
  exceptionNoteAtom,
  exceptionResolveTargetAtom,
  exceptionsAtom,
  exceptionsLoadingAtom,
  type ExceptionStatusFilter,
  exceptionsTotalAtom,
  exceptionSubmittingAtom,
} from './store';

export default function ExceptionsPage() {
  const list = useAtomValue(exceptionsAtom);
  const total = useAtomValue(exceptionsTotalAtom);
  const loading = useAtomValue(exceptionsLoadingAtom);
  const submitting = useAtomValue(exceptionSubmittingAtom);
  const filters = useAtomValue(exceptionFiltersAtom);
  const [resolveTarget, setResolveTarget] = useAtom(exceptionResolveTargetAtom);
  const [note, setNote] = useAtom(exceptionNoteAtom);
  const { loadExceptions, reloadExceptions, applyFilters, changePage, resolve } = useExceptionActions();

  useEffect(() => {
    void loadExceptions(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onResolve(): Promise<void> {
    if (!resolveTarget) return;
    const ok = await resolve(resolveTarget.id, note);
    if (ok) {
      toast.success('已标记处理');
      setResolveTarget(null);
      setNote('');
      void reloadExceptions();
    }
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
        title='异常池'
        description='超时、下单失败、停滞、仲裁、表外状态等自动进池；同一对象同一原因未处理时去重'
      />

      <div className='mb-3 flex flex-wrap items-center gap-2'>
        <Select
          value={filters.status}
          onValueChange={(v) => applyFilters({ status: v as ExceptionStatusFilter })}
        >
          <SelectTrigger className='w-32'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='open'>未处理</SelectItem>
            <SelectItem value='resolved'>已处理</SelectItem>
            <SelectItem value='all'>全部</SelectItem>
          </SelectContent>
        </Select>

        <Select
          value={filters.ref_type || '_all'}
          onValueChange={(v) => applyFilters({ ref_type: v === '_all' ? '' : v })}
        >
          <SelectTrigger className='w-32'>
            <SelectValue placeholder='对象类型' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部对象</SelectItem>
            <SelectItem value='order'>订单</SelectItem>
            <SelectItem value='purchase_task'>采购任务</SelectItem>
            <SelectItem value='shipment'>包裹</SelectItem>
          </SelectContent>
        </Select>

        <Select value={filters.code || '_all'} onValueChange={(v) => applyFilters({ code: v === '_all' ? '' : v })}>
          <SelectTrigger className='w-48'>
            <SelectValue placeholder='原因' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='_all'>全部原因</SelectItem>
            {EXCEPTION_CODE_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <DataTable
        data={list}
        loading={loading}
        getRowId={(e) => e.id}
        pagination={pagination}
        emptyText={filters.status === 'open' ? '异常池是空的，很好' : '没有符合条件的异常'}
        columns={[
          {
            id: 'ref',
            header: '对象',
            accessorFn: (e) => `${e.ref_type}:${e.ref_id}`,
            cell: ({ row }) => (
              <div className='text-xs'>
                <div>{REF_TYPE[row.original.ref_type] ?? row.original.ref_type}</div>
                <div className='font-mono text-muted-foreground'>{row.original.ref_id}</div>
              </div>
            ),
          },
          {
            id: 'code',
            header: '原因',
            accessorFn: (e) => e.code,
            cell: ({ row }) => exceptionCode(row.original.code),
          },
          {
            id: 'detail',
            header: '详情',
            accessorFn: (e) => e.detail ?? '',
            cell: ({ row }) => (
              <span className='line-clamp-2 max-w-md text-xs' title={row.original.detail ?? ''}>
                {row.original.detail ?? '—'}
              </span>
            ),
          },
          {
            id: 'status',
            header: '状态',
            accessorFn: (e) => e.status,
            cell: ({ row }) => (
              <StatusBadge
                spec={row.original.status === 'open'
                  ? { text: '未处理', tone: 'warning' }
                  : { text: '已处理', tone: 'success' }}
              />
            ),
          },
          {
            id: 'created',
            header: '进池时间',
            accessorFn: (e) => e.created_at,
            cell: ({ row }) => formatDateTime(row.original.created_at),
          },
          {
            id: 'handled',
            header: '处理',
            accessorFn: (e) => e.handled_at ?? '',
            cell: ({ row }) =>
              row.original.handled_at
                ? (
                  <div className='text-xs'>
                    <div>{row.original.handled_by}</div>
                    <div className='text-muted-foreground'>{formatDateTime(row.original.handled_at)}</div>
                    {row.original.note && <div className='text-muted-foreground'>备注：{row.original.note}</div>}
                  </div>
                )
                : (
                  '—'
                ),
          },
          {
            id: 'actions',
            header: '操作',
            cell: ({ row }) =>
              row.original.status === 'open'
                ? (
                  <Button
                    variant='secondary'
                    size='sm'
                    onClick={() => {
                      setNote('');
                      setResolveTarget(row.original);
                    }}
                  >
                    标记处理
                  </Button>
                )
                : null,
          },
        ]}
      />

      <ConfirmDialog
        open={!!resolveTarget}
        onOpenChange={(open) => !open && setResolveTarget(null)}
        title='标记已处理？'
        description={
          <div className='space-y-2'>
            <p>
              {exceptionCode(resolveTarget?.code)}：{resolveTarget?.detail ?? '—'}
            </p>
            <Textarea
              placeholder='处理说明（写进留痕）'
              value={note}
              onChange={(e) => setNote(e.target.value)}
              rows={3}
            />
          </div>
        }
        confirmText='标记已处理'
        busy={submitting}
        onConfirm={() => void onResolve()}
      />
    </div>
  );
}
