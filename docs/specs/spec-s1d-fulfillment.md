# spec-s1d-fulfillment —— S1-D 履约编排（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-D**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#9](https://github.com/Strelizialeomon/ozon-dropship/issues/9) ｜ 状态：v1.1（2026-10-07：PR #16 重审处置——实现修订 13 项、明确不改 1 项，见 §7 第 2 次；同日拼写同步 `hybrid` → `hybryd`，随 S1-B 实施修正，见总纲 §14 第 3 次）｜ 发布序：**第 2 批开发**，S1-A 合并后开工；与 B、C 并行开发，**须在 B、C 之后合并**
> 跨份验收与协作声明：S1 父 issue（待开）

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| `catalog`：货源商品与按店映射 | Ozon / 1688 客户端实现（S1-B、S1-C） |
| `order`：轮询拉单、状态映射、异常池、订单接口 | 推送、库存同步、跨境通道、免密支付（S2） |
| `purchase`：采购任务、自动 / 人工执行器、下单防重 | 退货、财务（S3 / S4） |
| `shipment`：中转点、交接、备货、面单、传单号 | 前端（S1-E） |

## 2. 地盘

- **独占**：`backend/internal/{order,purchase,shipment,catalog}/**`。
- **不碰**：`internal/{ozon,alibaba}`（B、C）、S1-A 的独占目录、`frontend/**`、`deploy/**`。
- **共享件**（先在 S1 父 issue 声明再改）：`internal/router` 的注册行、`cmd/api` 的装配行、如需新增迁移文件。
- **依赖方向**（[ADR-20261007-go-package-deps-v2](../decisions/2026-10-07-go-package-deps-v2.md)）：`store`、`catalog` 在下（上方各包都可用）；`order` 依赖 `store`；`purchase` 依赖 `order`、`catalog`、`store`；`shipment` 依赖 `order`、`purchase`、`store`（`purchase` 这条 2026-10-07 新增：交接对照表要国内快递号，只读不写）。新订单要生成采购任务：`order` 入库后投 asynq 任务，由 `purchase` 处理，不反向 import。
- **对 B、C 的调用**：按它们子 spec 的「对外方法清单」，在本份各包里定义所需的小接口；测试用假实现，装配层接真实客户端——所以开发不用等 B、C 合并。

## 3. 要做的事

1. **`catalog`**（总纲 §5.3）：`supplier_offers`、`offer_links` 的增删改查；S1 下单通道只用 `self_use` / `manual`。
2. **`order`**（总纲 §5.2）：每店每 5 分钟轮询拉单；按 `store_id + posting_number` upsert；按 §5.2 映射表逐状态执行；拉单成功后写回 `stores.last_sync_at`；异常池（S1 类型照总纲 §5.2 标注：超时未采购、1688 下单失败、地址校验失败、发货截止临近、国内段或中转点停滞、`ship_failed`、仲裁、表外状态；落 `exceptions` 表）；订单列表 / 详情 / 批量。
3. **`purchase`**（总纲 §5.1、§7.3）：
   - 采购任务状态机；自动执行器经 C「预览 → 下单」；S1 无免密支付——下单成功后任务停在 `ordered`，人工在 1688 付款后在任务台「记已付款（填实付）」推进 `paid`（可经 C `GetOrder` 核对），随后回填。
   - 人工执行器出备料单：收货地址 = 中转点、备注含 `posting_number`、**不含买家个人信息**；回填平台单号、实付、国内快递号，并校验格式。
   - 新商家首单自动转人工。
   - 下单防重：崩溃重启后，先经 C 查买家订单再决定补记还是下单。
4. **`shipment`**（总纲 §5.7、§7.4）：中转点增删改查；交接（货代仓导出对照表，自有仓扫码出面单）；签收 → `at_relay`；备货（经 B，复核 `substatus`）、取面单（经 B）、按 `tpl_integration_type` 决定是否传单号（经 B）→ `handed_over`。
5. 各包在 S1-A 的补投扫描框架里注册自己的超时规则（总纲 §5.5）。

**本份提供的操作台接口**（S1-E 照此手写）：

| 路径前缀 | 用途 |
|---|---|
| `/api/orders` | 订单工作台：列表 / 筛选 / 详情 / 批量 |
| `/api/exceptions` | 异常池：列表 / 处理 |
| `/api/purchase-tasks` | 采购任务台：执行 / 转人工 / 备料单 / 回填 |
| `/api/supplier-offers`、`/api/offer-links` | 映射与报价 |
| `/api/relay-points` | 中转点 |
| `/api/shipments` | 打包交接：导出对照表 / 签收 / 备货 / 面单 / 传单号 |

## 4. 验收

- [ ] 依赖方向符合包依赖 ADR：无反向 import，`go build` 无循环
- [ ] 拉单：同一 posting 重复拉只入库一次；§5.2 映射表每一行都有单测（含「平台未放行」不建采购任务、表外状态进异常池）；拉单成功后写回 `stores.last_sync_at`
- [ ] 采购：自动 / 人工两种执行器；新商家首单自动转人工；模拟「下单成功后崩溃」，重启后不重复下单；自动任务下单成功停在 `ordered`、「记已付款」后推进并回填
- [ ] 备料单：收货地址 = 中转点、备注含 `posting_number`、无买家个人信息；回填格式校验生效
- [ ] 中转：两类中转点的状态推进；备货返回成功但 `substatus = ship_failed` 时进异常池
- [ ] 传单号：`tpl_integration_type` 五个取值各有单测（`ozon` / `aggregator` 不传；`3pl_tracking` / `non_integrated` 传；`hybryd` 进异常池）
- [ ] 异常池：S1 各类异常能写入 `exceptions`、能手动处理、留审计
- [ ] 接口清单里每个接口都有 handler 测试

## 5. 机制清单

无新增机制。落地总纲 §5.1 采购任务、§5.2 订单状态机 + 异常队列、§5.3 供应商映射、§5.5 补投规则、§5.7 中转点交接。

## 6. 自定细节

- 新订单触发采购的 asynq 任务名 `purchase:plan`。
- 异常阈值默认：发货截止前 24 小时仍未交运；下单后 72 小时国内段无物流更新；中转点签收后 24 小时未交运。
- 货代仓交接对照表导出为 CSV。
- S1 自动任务的付款推进：任务台「记已付款」为主、`GetOrder` 核对为辅（总纲 §9·S1「人工确认付款」）。

## 7. 审核修订记录

PR #4 重审（owner 2026-10-07 点选「改」）的处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 3-6 | 轻微 | 异常清单漏「仲裁」「地址校验失败」 | 改：清单补齐；总纲 §5.2 已标波次归属 |
| 4-1 | 严重 | 异常池无存储载体 | 改：落 `exceptions` 表（总纲新增）；验收同步 |
| 4-3 | 中 | 同步时间无写方 | 改：拉单成功写 `stores.last_sync_at`；验收同步 |
| 4-8 | 轻微 | 「下单 → paid」推进未定义 | 改：定「记已付款」路径；验收同步 |

**第 2 次：重审（owner 2026-10-07 在 PR #16 点选「重审」，基准 issue-9 HEAD）**，原文见 PR #16 评论。两路：规格符合性（8 条 = 严重 1、中 3、轻微 4）、对抗式找 bug（14 条 = 严重 2、中 8、轻微 4）。owner 点选「全改（含轻微）+ 加唯一键迁移」，处置如下（表内 1-x = 路一发现，2-x = 路二发现）：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 1-1 | 严重 | 超时 / 停滞 / 补投扫描的计时器用 `orders.updated_at`，而轮询每轮无条件刷新它 → 判定永不触发 | 改：拉单只在字段真有变化时才写库、才动 `updated_at`；回归 `TestPollDoesNotRefreshUpdatedAtSoSweepFires` |
| 2-1 | 严重 | 一单多任务的防重核对只按 posting_number 匹配 → 兄弟任务的 1688 单被串号认领 | 改：下单留言加任务标记（`TradeRemark`），核对要求 posting + 标记同时命中；回归 `TestSiblingTasksDoNotCrossClaim` |
| 2-2 | 严重 | 并发拉单 → `order_items` 重复行 → 采购数量翻倍 | 改：迁移 `20261007130000` 加唯一键 `order_items(order_id, ozon_offer_id)` + 幂等 upsert；回归 `TestUpsertItemsIdempotentUnderConcurrentWrites` |
| 1-2 | 中 | 「记已付款」之后没有入口回填国内快递号（交接对照表拿不到单号） | 改：回填允许从 `paid` 进入，平台单号 / 实付在既有采购单缺时才必填；回归 `TestFillBackAfterMarkPaid` |
| 1-3 | 中 | 总纲 §5.2「异常判定（自动进池 **+ 通知**）」只落了进池 | 改：异常写入同时走飞书（去重键 = 对象 + 码）；回归 `TestExceptionRaiseNotifies` |
| 1-4 | 中 | `tpl_integration_type` 只认 `hybrid`，而官方原文是 `hybryd`（B 份实测） | 改：两版拼写都认（S1-B 已对拼写单独摆卡）；单测覆盖两个值 |
| 2-4 | 中 | 列表载荷缺字段时清空 `total_amount` / `ship_deadline` / `tpl_integration_type` | 改：更新路径「空值不清空」+ 无商品行的老单补拉详情 |
| 2-5 | 中 | 已取消 / 退货订单仍会被自动执行器真实下单，且任务关不掉 | 改：订单终态不再下单、未采购任务收掉（已采购的留人工核）；回归 `TestExecuteSkipsTerminalOrder` |
| 2-6 | 中 | 交接对照表静默截断在 200 行 | 改：翻页取全；回归 `TestHandoverCSVExportsAllPages` |
| 2-7 | 中 | 任务状态迁移无 CAS：人工与自动并发会状态倒退、采购单被覆盖 | 改：状态迁移全走 CAS（`SetStatusFrom`）+ 采购单唯一键 + 迟到自动结果不盖人工单号；回归 `TestLateRecordDoesNotRegress` |
| 2-8 | 中 | 同一订单可能写出两行 `shipments` | 改：迁移加唯一键 `shipments(order_id)` + 撞键重查；回归 `TestGetOrCreateSingleRowUnderConcurrency` |
| 2-9 | 中 | 拉未完成单失败被吞，`last_sync_at` 照前移 → 老单状态变化永久漏接 | 改：整轮失败、不写同步游标；回归 `TestUnfulfilledFailureFailsPollAndKeepsCursor` |
| 2-10 | 中 | `exception` 任务的「执行」接口静默无效（还回成功） | 改：执行入口可执行才投递、不可执行明确报错；`exception` 允许重试；回归 `TestTriggerExecuteStates` |
| 1-5 | 轻微 | 国内段停滞只扫 `shipped` | 改：`ordered` / `paid` / `shipped` 都扫；回归 `TestSweepCoversOrderedAndPaidTasks` |
| 1-6 | 轻微 | 两处实现期决定未进清单（`unknown_tpl` 码、中转点模型落 order 包） | 改：补进 PR 正文的自定细节 |
| 1-7 | 轻微 | 两个文件超出父单的共享件声明 | 改：已在父单 #5 补声明（含新增迁移） |
| 2-11 | 轻微 | 软删行占着唯一键 → `UpsertFromOzon` 无限递归（栈溢出会带崩进程） | 改：限次重试 + 明确报错；回归 `TestSoftDeletedOrderDoesNotRecurse` |
| 2-12 | 轻微 | `completed` 状态没有任何写方 | **不改**：总纲 §5.2 映射表里没有一条 Ozon 状态映射到 `completed`，S1 不造写方；等 S3（轨迹 / 退货）/ S4（对账）驱动 |
| 2-13 | 轻微 | 已 `delivered` 的订单仍会被 cancelled 旁支改回去 | 改：送达 / 完成之后不再被取消覆盖（拒收 / 退回属 S3 退货流程）；单测补两例 |
| 2-14 | 轻微 | 记采购单失败绕过 `retryOrFail` → 任务永远停在 `executing` | 改：记录失败走 `retryOrFail`（重试用尽进异常池） |

**新增迁移**：`backend/migrations/20261007130000_s1d_unique_keys.sql`（三个唯一键 + 历史去重），已在父单 #5 补声明；`goose up/down` 实测通过。
