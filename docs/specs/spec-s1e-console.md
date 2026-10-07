# spec-s1e-console —— S1-E 前端操作台（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-E**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#10](https://github.com/Strelizialeomon/ozon-dropship/issues/10) ｜ 状态：v1.0（2026-10-07）｜ 发布序：S1-A 合并后可开工；与 D 并行开发，**须在 D 之后合并**
> 跨份验收与协作声明：S1 父 issue [#5](https://github.com/Strelizialeomon/ozon-dropship/issues/5)

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

**第 2 次：实施期重审（owner 2026-10-07 在 PR #18 点选「重审」，基准 issue-10 HEAD）**，原文见 PR #18 的两条评论。两路：规格符合性（路一：严重 1、中 3、轻微 8）、对抗式找 bug（路二：严重 1、中 6、轻微 5；与路一重两条）。owner 点选「全改」，处置如下：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 1-1·2-7* | 严重 | D（#9）已合并而前端手写契约与实装系统性不符（信封、动作名、路径、面单、行字段） | 改：按 main@bc76506 的 Go 代码逐字段重写 5 组接口与相关页面；起真后端（独立库）跑通登录/列表/详情/批量建任务/转人工/备料单/签收/凭据/异常处理全链路 |
| 2-1 | 严重 | 记付款/回填/传单号三弹窗取消后表单残留，会把上一单的金额单号写进下一单 | 改：三个弹窗改 react-hook-form + 打开即 reset；加回归测试（取消换单后输入框为空） |
| 1-2 | 中 | 六个表单未按 ADR 用 react-hook-form + zod | 改：六表单全部改为 RHF + zod（顺带消掉无防重提交） |
| 1-3·2-2 | 中 | 凭据页「企业级」过滤与「全部」撞空串哨兵，永远看不到企业级 | 改：`_all`/`_ent` 哨兵 + `credentialStoreParam` 映射；凭据弹窗「归属」下拉同修 |
| 1-4·2-8 | 中 | 店铺下拉一次性缓存，建店后其他页面看不到 | 改：`invalidate/refreshStores`，店铺增删改后刷新（联调用新建店→映射下拉已验） |
| 2-3 | 中 | 回填/记付款/传单号/异常处理/删除确认无提交防重 | 改：RHF `isSubmitting` + ConfirmDialog 接上 `busy`（异常池、映射删除的原子此前只写不读） |
| 2-4 | 中 | 删除货源/映射后列表不重拉 | 改：删除成功后 reload |
| 2-5 | 中 | 保存/轮换凭据后列表不刷新 | 改：保存后 `reloadCredentials()` |
| 2-6 | 中 | 备料单请求无竞态保护（后点被旧响应覆盖，会拿错单） | 改：`prepSeq` 守卫 |
| 2-7 | 中 | DataTable 排序在真实页面点不动（列无 accessor） | 改：全部页面列补 `accessorFn` + 回归测试 |
| 1-5 | 轻微 | 「目录照 xhs-analysis」的 `src/styles` 是文件不是目录 | 改：`src/styles.css` → `src/styles/index.css` |
| 1-6 | 轻微 | 守卫注释照抄「包在 `<Routes>` 外层」与实现不符 | 改：注释如实写「`<Routes>` 内布局路由（功能等价）」 |
| 1-7 | 轻微 | 若干自定数值未申报（300ms 线、30 分钟线、快递号正则） | 改：快递号正则改为与后端逐字一致（不再自造）；300ms/30 分钟为测量与展示口径，写在 `perf/run.mjs`、`pages/system/index.tsx` 注释与 PR 正文 |
| 1-8 | 轻微 | 「jotai v3 兼容 4/4 绿」口径打架 | 改：#5 实测①评论更正为准确口径（房屋风格 2 用例 8 断言 + atoms 层共 4 用例） |
| 1-9 | 轻微 | 冒烟脚本仓内无载体 | 不改：会话级临时工具；改以**真后端联调**（本次）替代，结论见 PR #18 |
| 1-10 | 轻微 | 异常码/物流类型标签与 D 常量不一致 | 改：`labels.ts` 对齐后端 12 码全量；`hybryd` 按官方原文拼写 |
| 1-11 | 轻微 | 映射表单手填货源 ID | 改：改为从货源列表下拉选择 |
| 1-12 | 轻微 | 订单/映射页越组调 `/api/stores` | 不改：店铺下拉跨页必需；记此备查 |
| 2-9 | 轻微 | 「重置」不清关键字输入框 | 改：重置同时清空输入框 |
| 2-10 | 轻微 | 页码越界不回退（「第 2/1 页」空表） | 改：四个列表 load 支持越界回退到最后一页 |
| 2-11 | 轻微 | 动作后重拉用提交时刻的筛选快照 | 改：`reload*` 用 `getDefaultStore()` 读「此刻」筛选 |
| 2-12 | 轻微 | 统一提示优先取 `error` 把中文 message 挤掉 | 改：`message || error` |
| 2-13 | 轻微 | 面单在 await 后开窗会被浏览器拦 | 改：同步开空窗 → 取 PDF blob 换入（后端本就是 PDF 二进制）；顺带修了 ConfirmDialog `<p>` 内嵌块级内容的 DOM 嵌套违规（联调冒烟抓到） |

\* 1-1 与 2-7 实为同一件事（路一先从契约面发现、路二从列定义面切入），处置合并。
