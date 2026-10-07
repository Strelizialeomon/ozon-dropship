# spec-s1e-console —— S1-E 前端操作台（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-E**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: 待开 ｜ 状态：v1.0（2026-10-07）｜ 发布序：S1-A 合并后可开工；与 D 并行开发，**须在 D 之后合并**
> 跨份验收与协作声明：S1 父 issue（待开）

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| `frontend/` 整个子项目：骨架 + S1 页面 | 后端（S1-A ~ S1-D） |
| 照 A、D 的接口清单手写类型与调用 | 部署（S1-F） |
| 两项开工实测（jotai v3、大表格性能） | 退货、财务、刊登等后续波次页面 |

## 2. 地盘

- **独占**：`frontend/**`。
- **不碰**：`backend/**`、`deploy/**`。

## 3. 要做的事

1. **骨架**（照 [前端 ADR](../decisions/2026-10-07-frontend-stack.md)）：Bun + Rsbuild + React + React Router 声明式（`<BrowserRouter>` + `<Routes>`）+ jotai + shadcn/ui（TanStack Table、react-hook-form + zod、Tailwind v4）+ Axios；dprint；bun test + happy-dom + testing-library。目录照 xhs-analysis：`src/api`、`atoms`、`components`、`lib`、`pages`、`router`、`styles`。jotai 按 hi-jotai 写法规范。
2. **S1 页面**（总纲 §8）：

   | 页面 | 调哪组接口 |
   |---|---|
   | 登录 | `/api/auth`（A） |
   | 订单工作台 | `/api/orders`（D） |
   | 采购任务台（含备料单、回填） | `/api/purchase-tasks`（D） |
   | 异常池 | `/api/exceptions`（D） |
   | 映射与报价 | `/api/supplier-offers`、`/api/offer-links`（D） |
   | 中转点与打包 | `/api/relay-points`、`/api/shipments`（D） |
   | 店铺与凭据 | `/api/stores`、`/api/credentials`（A） |
   | 系统状态 | `/api/system`、队列监控页入口（A） |

3. **类型与调用**：照接口清单 + 后端 Go 代码手写，不用生成工具。
4. **开工实测**（结论贴 S1 父 issue）：jotai v3 与 hi-jotai 规范是否兼容（不兼容锁 2.19.1）；shadcn + TanStack Table 千行表格性能。

## 4. 验收

- [ ] `bun run build`、`bun test`、`dprint check` 全绿
- [ ] 每个 S1 页面都能对真实后端完成主操作（完整闭环在父 issue 联调验收）
- [ ] 未登录或会话失效跳登录页；operator 看不到 admin 菜单
- [ ] 凭据只显示脱敏尾号；到期 ≤ 14 天高亮
- [ ] 刷新任意路由不 404（配合 S1-F 的 Caddy 回退）
- [ ] 两项开工实测结论已贴父 issue

## 5. 机制清单

无新增机制。落地总纲 §8 操作台（双轨的手动侧）。

## 6. 自定细节

- Axios 用一个统一实例：401 跳登录，其余错误弹统一提示。
- 登录守卫包在 `<Routes>` 外层。
- 千行表格的性能线：1000 行首次渲染 < 1 秒，筛选、排序无明显卡顿。
- 目录结构照 xhs-analysis：`src/{api,atoms,components,lib,pages,router,styles}`；构建产物 `dist/`（rsbuild 默认，供 S1-F 的 Caddy 指向）。

## 7. 审核修订记录

PR #4 重审（owner 2026-10-07 点选「改」）的处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 3-7 | 轻微 | 自定细节缺前端目录结构 | 改：已补（含构建产物目录 `dist/`） |
| 4-15 | 轻微 | Caddy 指向的产物目录未写死 | 改：定 `dist/`，与 S1-F 同步 |
