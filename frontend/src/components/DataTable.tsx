import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type RowSelectionState,
  type SortingState,
  useReactTable,
} from '@tanstack/react-table';
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { cn } from '@/lib/utils';

/**
 * 通用数据表格（shadcn Table + TanStack Table 自建，ADR 已接受此代价）。
 * - 排序：客户端，点表头切换（当前页内）。
 * - 分页：服务端，传 `pagination` 走受控页码；不传则不分页（整表渲染，供千行表格等场景）。
 * - 行选择：传 rowSelection/onRowSelectionChange 开启勾选列（批量操作用）。
 */

export interface ServerPagination {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
}

export interface DataTableProps<TData> {
  columns: ColumnDef<TData, unknown>[];
  data: TData[];
  loading?: boolean;
  emptyText?: string;
  getRowId?: (row: TData) => string;
  onRowClick?: (row: TData) => void;
  pagination?: ServerPagination;
  rowSelection?: RowSelectionState;
  onRowSelectionChange?: (next: RowSelectionState) => void;
  className?: string;
}

/** 勾选列：配合 rowSelection 受控使用。 */
export function selectionColumn<TData>(): ColumnDef<TData, unknown> {
  return {
    id: '_select',
    enableSorting: false,
    size: 32,
    header: ({ table }) => (
      <Checkbox
        aria-label='全选'
        checked={table.getIsAllPageRowsSelected() || (table.getIsSomePageRowsSelected() ? 'indeterminate' : false)}
        onCheckedChange={(v) => table.toggleAllPageRowsSelected(v === true)}
      />
    ),
    cell: ({ row }) => (
      <div onClick={(e) => e.stopPropagation()} className='flex items-center'>
        <Checkbox
          aria-label='选择本行'
          checked={row.getIsSelected()}
          onCheckedChange={(v) => row.toggleSelected(v === true)}
        />
      </div>
    ),
  };
}

export function DataTable<TData>({
  columns,
  data,
  loading = false,
  emptyText = '暂无数据',
  getRowId,
  onRowClick,
  pagination,
  rowSelection,
  onRowSelectionChange,
  className,
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([]);

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      ...(rowSelection ? { rowSelection } : {}),
    },
    getRowId,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    onSortingChange: setSorting,
    ...(onRowSelectionChange
      ? {
        enableRowSelection: true,
        onRowSelectionChange: (updater) => {
          const next = typeof updater === 'function' ? updater(rowSelection ?? {}) : updater;
          onRowSelectionChange(next);
        },
      }
      : {}),
    ...(pagination
      ? {
        manualPagination: true,
        pageCount: Math.max(1, Math.ceil(pagination.total / pagination.pageSize)),
      }
      : {}),
  });

  const rows = table.getRowModel().rows;
  const colCount = table.getAllLeafColumns().length;
  const showSkeleton = loading && data.length === 0;
  const pageCount = pagination ? Math.max(1, Math.ceil(pagination.total / pagination.pageSize)) : 1;

  return (
    <div className={cn('space-y-2', className)}>
      <div className='overflow-x-auto rounded-md border bg-background'>
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((hg) => (
              <TableRow key={hg.id}>
                {hg.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    style={header.column.columnDef.size ? { width: header.column.columnDef.size } : undefined}
                  >
                    {header.isPlaceholder
                      ? null
                      : header.column.getCanSort()
                      ? (
                        <button
                          type='button'
                          className='flex items-center gap-1 hover:text-foreground'
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {header.column.getIsSorted() === 'asc'
                            ? <ArrowUp className='size-3.5' />
                            : header.column.getIsSorted() === 'desc'
                            ? <ArrowDown className='size-3.5' />
                            : <ArrowUpDown className='size-3.5 opacity-40' />}
                        </button>
                      )
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {showSkeleton
              ? Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: colCount }).map((__, j) => (
                    <TableCell key={j}>
                      <Skeleton className='h-5 w-full' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
              : rows.length === 0
              ? (
                <TableRow>
                  <TableCell colSpan={colCount} className='h-24 text-center text-muted-foreground'>
                    {loading ? '加载中…' : emptyText}
                  </TableCell>
                </TableRow>
              )
              : rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() ? 'selected' : undefined}
                  className={onRowClick ? 'cursor-pointer' : undefined}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                  ))}
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>

      {pagination && (
        <div className='flex items-center justify-between text-sm text-muted-foreground'>
          <span>共 {pagination.total} 条</span>
          <div className='flex items-center gap-2'>
            <span>
              第 {pagination.page} / {pageCount} 页
            </span>
            <Button
              variant='outline'
              size='icon'
              className='size-7'
              disabled={pagination.page <= 1 || loading}
              onClick={() => pagination.onPageChange(pagination.page - 1)}
            >
              <ChevronLeft className='size-4' />
            </Button>
            <Button
              variant='outline'
              size='icon'
              className='size-7'
              disabled={pagination.page >= pageCount || loading}
              onClick={() => pagination.onPageChange(pagination.page + 1)}
            >
              <ChevronRight className='size-4' />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
