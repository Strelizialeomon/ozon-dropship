// 开工实测（S1-E，子 spec §3.4）：shadcn Table + TanStack Table 千行表格性能。
//
// 用真实组件（src/components/DataTable）渲染 1000 行（生产构建 + 真浏览器，见 perf/run.mjs），
// 在页内用 performance.now() + 双 rAF（近似「画完」）测三件事：
//   ① 1000 行首次渲染（空表 → 1000 行画完）——验收线：< 1 秒
//   ② 点表头排序（1000 行重排 + 重画）——「无明显卡顿」按 < 300ms 记（3 次取中位）
//   ③ 筛选（1000 行 → 约 120 行子集重画 + 复位）——同上
// 结论贴 S1 父 issue #5。

import { type ColumnDef } from '@tanstack/react-table';
import ReactDOM from 'react-dom/client';
import { useEffect, useMemo, useRef, useState } from 'react';
import { DataTable } from '@/components/DataTable';
import { StatusBadge } from '@/components/StatusBadge';
import { orderStatus } from '@/lib/labels';
import '../src/styles.css';

interface Row {
  id: string;
  posting_number: string;
  store: string;
  status: string;
  amount: number;
  deadline: string;
}

const STATUSES = ['new', 'purchasing', 'inbound', 'at_relay', 'in_transit', 'delivered'];

function makeRows(n: number): Row[] {
  return Array.from({ length: n }, (_, i) => ({
    id: String(i + 1),
    posting_number: `OZ-${String(100000 + i)}`,
    store: `店铺 ${String.fromCharCode(65 + (i % 8))}`,
    status: STATUSES[i % STATUSES.length],
    amount: Math.round((i * 37) % 20000) / 100,
    deadline: new Date(Date.now() + (i % 120) * 3600_000).toISOString(),
  }));
}

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'posting_number', header: 'posting', cell: ({ row }) => row.original.posting_number },
  { accessorKey: 'store', header: '店铺', cell: ({ row }) => row.original.store },
  {
    accessorKey: 'status',
    header: '状态',
    cell: ({ row }) => <StatusBadge spec={orderStatus(row.original.status)} />,
  },
  {
    accessorKey: 'amount',
    header: '金额',
    cell: ({ row }) => row.original.amount.toFixed(2),
  },
  {
    accessorKey: 'deadline',
    header: '截止',
    cell: ({ row }) => row.original.deadline.slice(5, 16).replace('T', ' '),
  },
];

interface PerfResult {
  rowCount: number;
  firstRenderMs: number;
  sortMs: number[];
  filterMs: number[];
  resetMs: number[];
  done: boolean;
}

declare global {
  interface Window {
    __perf?: PerfResult;
  }
}

function nextPaint(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
}

async function measure(fn: () => void): Promise<number> {
  const t0 = performance.now();
  fn();
  await nextPaint();
  return performance.now() - t0;
}

function round(ms: number): number {
  return Math.round(ms * 10) / 10;
}

function PerfPage() {
  const allRows = useMemo(() => makeRows(1000), []);
  const filteredRows = useMemo(() => allRows.filter((r) => r.store === '店铺 A'), [allRows]);
  const [rows, setRows] = useState<Row[]>([]);
  const running = useRef(false);

  useEffect(() => {
    if (running.current) return; // StrictMode 双跑防护（本次未开 StrictMode，留着不吃亏）
    running.current = true;
    void (async () => {
      const result: PerfResult = {
        rowCount: 1000,
        firstRenderMs: 0,
        sortMs: [],
        filterMs: [],
        resetMs: [],
        done: false,
      };
      window.__perf = result;

      result.firstRenderMs = round(await measure(() => setRows(allRows)));

      // 排序：点「金额」表头（第 4 列）三次
      for (let i = 0; i < 3; i++) {
        result.sortMs.push(
          round(
            await measure(() => {
              const btn = document.querySelector<HTMLButtonElement>('[data-perf] thead th:nth-child(4) button');
              btn?.click();
            }),
          ),
        );
        await new Promise((r) => setTimeout(r, 80));
      }

      // 筛选：1000 → 约 125 行（店铺 A）
      for (let i = 0; i < 3; i++) {
        result.filterMs.push(round(await measure(() => setRows(filteredRows))));
        await new Promise((r) => setTimeout(r, 80));
        result.resetMs.push(round(await measure(() => setRows(allRows))));
        await new Promise((r) => setTimeout(r, 80));
      }

      result.done = true;
    })();
  }, [allRows, filteredRows]);

  return (
    <div data-perf className='p-4'>
      <DataTable columns={columns} data={rows} getRowId={(r) => r.id} emptyText='等待压测…' />
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('perf-root')!).render(<PerfPage />);
