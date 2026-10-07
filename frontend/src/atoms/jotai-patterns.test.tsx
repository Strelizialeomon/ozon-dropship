// 开工实测（S1-E，子 spec §3.4）：jotai v3 与 hi-jotai 房屋风格是否兼容。
//
// 本文件把房屋风格的核心写法逐条写出来跑一遍：
//   ① atom 只在 store 定义、② 读用 useAtomValue（精确订阅）、③ 写走 useXxxActions 纯函数、
//   ④ derived atom 跨组件共享、⑤ SaveState 枚举、⑥ 受控 Modal 存 null | {mode}、
//   ⑦ useAtom 管组件自有 UI 微状态、⑧ setter updater 函数式更新。
// 全绿 = 兼容（结论贴 S1 父 issue）。

import { describe, expect, it } from 'bun:test';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { atom, useAtom, useAtomValue, useSetAtom } from 'jotai';

// ── store：照房屋风格，atom 全部在模块级定义 ─────────────────────────────
interface Row {
  id: string;
  title: string;
  done: boolean;
}

const rowsAtom = atom<Row[]>([]);
const rowsLoadingAtom = atom<boolean>(false);
type SaveState = 'idle' | 'saving' | 'saved' | 'error';
const saveStateAtom = atom<SaveState>('idle');
const modalAtom = atom<null | { mode: 'create' } | { mode: 'edit'; id: string }>(null);

// derived：跨组件共享的派生才上升为 atom
const doneCountAtom = atom((get) => get(rowsAtom).filter((r) => r.done).length);

// 精确订阅探针：只订一个「无关」atom 的组件
const unrelatedAtom = atom(0);

let listRenders = 0;
let unrelatedRenders = 0;
let seq = 0;

function RowList() {
  const rows = useAtomValue(rowsAtom);
  const done = useAtomValue(doneCountAtom);
  const loading = useAtomValue(rowsLoadingAtom); // loading 三件套：首屏
  listRenders++;
  return (
    <div>
      <span data-testid='rows'>{rows.map((r) => r.title).join(',')}</span>
      <span data-testid='done'>{done}</span>
      {loading && <span data-testid='loading'>…</span>}
    </div>
  );
}

function Unrelated() {
  const v = useAtomValue(unrelatedAtom);
  unrelatedRenders++;
  return <span data-testid='unrelated'>{v}</span>;
}

// 写：房屋风格允许的两种形态——actions 纯函数 + 组件自有 UI 微状态的 useSetAtom
function useRowActions() {
  const setRows = useSetAtom(rowsAtom);
  const setSaveState = useSetAtom(saveStateAtom);

  async function createRow(title: string) {
    setSaveState('saving');
    const row: Row = { id: `_new_${++seq}`, title, done: false };
    setRows((prev) => [row, ...prev]); // 乐观新增：函数式更新
    setSaveState('saved');
  }

  function toggleDone(id: string) {
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, done: !r.done } : r))); // setter updater
  }

  return { createRow, toggleDone };
}

function Toolbar() {
  const { createRow, toggleDone } = useRowActions();
  const [modal, setModal] = useAtom(modalAtom); // 组件自有 UI 微状态：useAtom
  const saveState = useAtomValue(saveStateAtom);
  return (
    <div>
      <button data-testid='open' onClick={() => setModal({ mode: 'create' })}>打开</button>
      <button
        data-testid='confirm'
        onClick={() => {
          void createRow('新行');
          setModal(null);
        }}
      >
        确认
      </button>
      <button data-testid='toggle' onClick={() => toggleDone('_new_1')}>切完成</button>
      <span data-testid='modal'>{modal ? modal.mode : 'closed'}</span>
      <span data-testid='save-state'>{saveState}</span>
    </div>
  );
}

describe('hi-jotai 房屋风格 × jotai v3', () => {
  it('读（useAtomValue + derived）、写（actions 纯函数）、保留 UI 微状态（useAtom）全部工作', async () => {
    const user = userEvent.setup();
    render(
      <>
        <RowList />
        <Toolbar />
        <Unrelated />
      </>,
    );

    expect(screen.getByTestId('rows').textContent).toBe('');
    expect(screen.getByTestId('done').textContent).toBe('0');
    expect(screen.getByTestId('save-state').textContent).toBe('idle');

    // Modal 开关（useAtom 受控）
    await user.click(screen.getByTestId('open'));
    expect(screen.getByTestId('modal').textContent).toBe('create');

    // 写入：actions 纯函数 → 列表 + derived 同步更新
    const listBefore = listRenders;
    await user.click(screen.getByTestId('confirm'));
    expect(screen.getByTestId('rows').textContent).toBe('新行');
    expect(screen.getByTestId('save-state').textContent).toBe('saved');
    expect(listRenders).toBeGreaterThan(listBefore);

    // 精确订阅：无关 atom 的组件没有被拖着重渲
    expect(unrelatedRenders).toBe(1);

    // 函数式更新
    await act(async () => {
      screen.getByTestId('toggle').click();
    });
    expect(screen.getByTestId('done').textContent).toBe('1');
  });

  it('write-only atom 形态（atom(null, (get, set, arg) => ...)）可用', async () => {
    // Taro profile 的写-atom 形态；web profile 虽然不用，但确认 v3 没删这条路径。
    const counterAtom = atom(0);
    const bumpAtom = atom(null, (get, set, by: number) => {
      set(counterAtom, get(counterAtom) + by);
    });

    function Probe() {
      const n = useAtomValue(counterAtom);
      const bump = useSetAtom(bumpAtom);
      return <button data-testid='bump' onClick={() => bump(2)}>{n}</button>;
    }

    render(<Probe />);
    const btn = screen.getByTestId('bump');
    await act(async () => {
      btn.click();
    });
    expect(btn.textContent).toBe('2');
  });
});
