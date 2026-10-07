// DataTable：排序点表头能工作、空态文案、分页受控回调。
import { describe, expect, it } from 'bun:test';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ColumnDef } from '@tanstack/react-table';
import { DataTable } from './DataTable';

interface Row {
  id: string;
  name: string;
  n: number;
}

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'name', header: '名称', cell: ({ row }) => row.original.name },
  { accessorKey: 'n', header: '数值', cell: ({ row }) => row.original.n },
];

const rows: Row[] = [
  { id: 'a', name: '甲', n: 3 },
  { id: 'b', name: '乙', n: 1 },
  { id: 'c', name: '丙', n: 2 },
];

function names(): string[] {
  return screen.getAllByRole('row').slice(1).map((r) => r.querySelector('td')?.textContent ?? '');
}

describe('DataTable', () => {
  it('点表头排序：数值列先降序（TanStack 对数字列默认 desc 优先），再切升序', async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} data={rows} getRowId={(r) => r.id} />);

    expect(names()).toEqual(['甲', '乙', '丙']);

    await user.click(screen.getByRole('button', { name: /数值/ }));
    expect(names()).toEqual(['甲', '丙', '乙']); // n: 3,2,1

    await user.click(screen.getByRole('button', { name: /数值/ }));
    expect(names()).toEqual(['乙', '丙', '甲']); // n: 1,2,3
  });

  it('空数据给空态文案', () => {
    render(<DataTable columns={columns} data={[]} emptyText='啥也没有' />);
    expect(screen.queryByText('啥也没有')).toBeTruthy();
  });

  it('分页受控：点下一页回调新页码', async () => {
    const pages: number[] = [];
    const user = userEvent.setup();
    render(
      <DataTable
        columns={columns}
        data={rows}
        getRowId={(r) => r.id}
        pagination={{ page: 1, pageSize: 3, total: 50, onPageChange: (p) => pages.push(p) }}
      />,
    );
    expect(screen.getByText('共 50 条')).toBeTruthy();
    const buttons = screen.getAllByRole('button');
    await user.click(buttons[buttons.length - 1]); // 下一页（最后一个按钮）
    expect(pages).toEqual([2]);
  });
});
