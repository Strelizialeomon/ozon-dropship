# frontend —— 履约中台操作台

React + Bun + Rsbuild + React Router（声明式）+ jotai + shadcn/ui（Tailwind v4）+ Axios。
选型与理由见 [ADR-20261007-frontend-stack](../docs/decisions/2026-10-07-frontend-stack.md)；页面范围见
[spec-s1e-console](../docs/specs/spec-s1e-console.md)。

## 跑起来

```bash
bun install
bun run dev        # 开发服务器（:3000），/api 代理到本机 Go（:8080，可用 API_PROXY_TARGET 覆盖）
bun run build      # 生产构建 → dist/（生产由 Caddy 指向它）
bun run test       # bun test（happy-dom + testing-library，preload 见 test-setup.ts）
bun run typecheck  # tsc --noEmit
bun run fmt        # dprint 格式化；CI/验收用 bunx dprint check
```

后端没起时页面会明着报错（401 会被送回登录页）——不内置假数据适配器，联调一律对真后端。

## 目录

```
src/
  api/        # 手写接口层：client.ts（统一 axios 实例 + 响应壳解包 + 401/错误提示策略）+ 各域 DTO 与纯请求函数
  atoms/      # 跨 feature 共享 atom（当前用户、店铺下拉选项）与对应 actions
  components/ # 共享组件：DataTable（TanStack Table 封装）、StatusBadge、ConfirmDialog、PageHeader、layout/AppShell
    ui/       # shadcn/ui 组件（registry 生成 + 导入改写，见下）
  lib/        # utils(cn) / format(时间金额) / labels(枚举→中文)
  pages/      # 每个页面一个 feature 目录：store.ts(atoms) + actions.ts + index.tsx + components/
  router/     # BrowserRouter + RequireAuth（登录守卫，布局路由）
```

写代码的约定（jotai 房屋风格、读写边界、命名）按 hi-jotai 规范；表单字段用 react-hook-form + zod，
业务数据读 `useAtomValue`、写走 `useXxxActions`。

## 两处“非标准”实现，动机都写在代码注释里

- **shadcn 组件是 registry 源码改写导入**：2026 版 shadcn registry 已切到 `radix-ui` 统一包 + `cn` npm 包，
  本仓沿用分体包（`@radix-ui/react-*`）与 `@/lib/utils` 的 `cn`，
  组件本体（new-york-v4）保持与 registry 一致，只改了 import 行。React 19（registry 源码按 ref-as-prop 写）。
- **`perf/` 是千行表格性能实测工具**（开工实测）：`bun perf/run.mjs` 构建 → 起静态服务 → 无头 chromium
  跑 1000 行渲染/排序/筛选并判红绿。产物落 `dist-perf/`，不进生产 `dist/`。
