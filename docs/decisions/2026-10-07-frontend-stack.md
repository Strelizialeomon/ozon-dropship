# ADR-20261007-frontend-stack：操作台前端用 React + Bun + Rsbuild + 声明式 React Router + jotai + shadcn/ui + Axios

- 状态：生效中
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：`frontend/` 全部代码
- 取代：无
- 被取代：无
- 决策来源：2026-10-07 会话（owner 点名 Bun / Rsbuild / React Router / jotai / shadcn/ui / Axios；逐项选择卡拍板其余各项）

## 决定

| 部件 | 选型 |
|---|---|
| 框架 / 包管理 / 构建 | React、Bun、Rsbuild（内置 Rspack） |
| 路由 | React Router 声明式写法：`<BrowserRouter>` + `<Routes>`（同 xhs-analysis、jasmine-lottery） |
| 状态与接口数据 | 只用 jotai（按 owner 的 jotai 写法规范），不加 TanStack Query |
| 组件库 | shadcn/ui 官方配法：表格 TanStack Table，表单 react-hook-form + zod，样式 Tailwind v4 |
| 请求 | Axios |
| 接口类型 | 全部手写（不用 tygo / orval 等生成） |
| 格式化 / 测试 | 沿用 xhs-analysis：dprint；bun test + happy-dom + testing-library |

## 依据

- 内部操作台、登录后使用，不需要服务端渲染；owner 现有项目几乎都用声明式路由，对外站点另用 Next.js 单独起项目。
- 工具与 owner 现有项目保持一致，少学新工具。

## 否决了什么

- antd v6（推荐过）、Vite、TanStack Router、TanStack Query。
- React Router 框架模式与 data 模式。
- 原生 fetch / ky（owner 选保留 Axios）；orval、tygo 生成类型。
- Biome + Vitest。

## 接受的代价

- shadcn 没有现成数据表格和中文文案，表格交互基于 TanStack Table 自建；
- jotai v3 于 2026-09-08 发布，写法规范要先核兼容（不兼容锁 2.19.1）；
- 声明式路由的出错页、「未保存离开」拦截要自己做；BrowserRouter 需 Caddy 配回退到 `index.html`；
- 类型手写，后端改字段要人工同步。

## 允许重开的条件

- 需要服务端渲染或对外公开页面（届时优先按惯例另起 Next.js 项目，或按 React Router 官方指南升级到框架模式）；
- shadcn + TanStack Table 的大表格性能实测不达标；
- jotai v3 与写法规范不兼容且无法修。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4.1、§8
