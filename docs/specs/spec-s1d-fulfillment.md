# spec-s1d-fulfillment —— S1-D 履约编排（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-D**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: 待开 ｜ 状态：v1.0（2026-10-07）｜ 发布序：**第 2 批开发**，S1-A 合并后开工；与 B、C 并行开发，**须在 B、C 之后合并**
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
- **依赖方向**（[ADR-20261007-go-package-deps](../decisions/2026-10-07-go-package-deps.md)）：`catalog` 在下；`order` 依赖 `store`；`purchase` 依赖 `order`、`catalog`；`shipment` 依赖 `order`。新订单要生成采购任务：`order` 入库后投 asynq 任务，由 `purchase` 处理，不反向 import。
- **对 B、C 的调用**：按它们子 spec 的「对外方法清单」，在本份各包里定义所需的小接口；测试用假实现，装配层接真实客户端——所以开发不用等 B、C 合并。

## 3. 要做的事

1. **`catalog`**（总纲 §5.3）：`supplier_offers`、`offer_links` 的增删改查；S1 下单通道只用 `self_use` / `manual`。
2. **`order`**（总纲 §5.2）：每店每 5 分钟轮询拉单；按 `store_id + posting_number` upsert；按 §5.2 映射表逐状态执行；异常池（S1 类型：超时未采购、1688 下单失败、`ship_failed`、表外状态、发货截止临近、国内段或中转点停滞）；订单列表 / 详情 / 批量。
3. **`purchase`**（总纲 §5.1、§7.3）：
   - 采购任务状态机；自动执行器经 C「预览 → 下单」，人工在 1688 付款后回填。
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
- [ ] 拉单：同一 posting 重复拉只入库一次；§5.2 映射表每一行都有单测（含「平台未放行」不建采购任务、表外状态进异常池）
- [ ] 采购：自动 / 人工两种执行器；新商家首单自动转人工；模拟「下单成功后崩溃」，重启后不重复下单
- [ ] 备料单：收货地址 = 中转点、备注含 `posting_number`、无买家个人信息；回填格式校验生效
- [ ] 中转：两类中转点的状态推进；备货返回成功但 `substatus = ship_failed` 时进异常池
- [ ] 传单号：`tpl_integration_type` 五个取值各有单测（`ozon` / `aggregator` 不传；`3pl_tracking` / `non_integrated` 传；`hybrid` 进异常池）
- [ ] 异常池：S1 各类异常能进池、能手动处理、留审计
- [ ] 接口清单里每个接口都有 handler 测试

## 5. 机制清单

无新增机制。落地总纲 §5.1 采购任务、§5.2 订单状态机 + 异常队列、§5.3 供应商映射、§5.5 补投规则、§5.7 中转点交接。

## 6. 自定细节

- 新订单触发采购的 asynq 任务名 `purchase:plan`。
- 异常阈值默认：发货截止前 24 小时仍未交运；下单后 72 小时国内段无物流更新；中转点签收后 24 小时未交运。
- 货代仓交接对照表导出为 CSV。

## 7. 审核修订记录

（审核完成后追加：# / 严重度 / 发现 / 处置）
