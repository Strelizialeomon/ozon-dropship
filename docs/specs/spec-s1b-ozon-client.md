# spec-s1b-ozon-client —— S1-B Ozon 客户端（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-B**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#7](https://github.com/Strelizialeomon/ozon-dropship/issues/7) ｜ 状态：v1.1（2026-10-07：实施期修正——混合方案拼写、ShipPosting 多包裹表述，见 §8）｜ 发布序：**第 2 批**，S1-A 合并后开工；与 C、D、F 并行
> 跨份验收与协作声明：S1 父 issue [#5](https://github.com/Strelizialeomon/ozon-dropship/issues/5)

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| `internal/ozon`：S1 用到的 Ozon Seller API 调用 | 何时拉单、状态怎么映射、何时备货（S1-D） |
| 鉴权头、接 S1-A 的限流器、429 处理、错误归一 | 推送、库存、财务、退货、刊登、取消、拆单、FBP 拉单接口（后续波次；FBP 接入在 S2） |
| 两项开工实测（发运单、本土店） | 凭据存取（S1-A 的 `store`；本包由调用方传入凭据） |

## 2. 地盘

- **独占**：`backend/internal/ozon/**`。
- **不碰**：其余全部。
- **共享件**：把 `GetRoles` 接到 S1-A 的到期检查，需在 `cmd/api` 加一行装配——先在 S1 父 issue 声明。
- **依赖方向**：本包只依赖 `internal/infra`，不 import 任何业务包（[ADR-20261007-go-package-deps](../decisions/2026-10-07-go-package-deps.md)）。

## 3. 对外方法清单（S1-D 据此在使用方定义小接口）

| 方法 | 接口 | 用途 |
|---|---|---|
| `ListPostings` | `/v4/posting/fbs/list` | 按时间窗、状态拉单 |
| `ListUnfulfilled` | `/v4/posting/fbs/unfulfilled/list` | 拉未完成单 |
| `GetPosting` | `/v3/posting/fbs/get` | 单详情：`status`、`substatus`、`tpl_integration_type`、`shipment_date` 等 |
| `ShipPosting` | `/v4/posting/fbs/ship`（多包裹用请求里的 `packages` 数组） | 备货；返回后由调用方复核 `substatus` |
| `GetPackageLabel` | `/v2/posting/fbs/package-label` | 面单 PDF |
| `SetTrackingNumber` | `/v2/fbs/posting/tracking-number/set` | 传单号（调用方按 `tpl_integration_type` 决定调不调） |
| `GetRoles` | `/v1/roles` | 密钥角色与到期时间 |
| `ListDeliveryMethods` | `/v2/delivery-method/list` | 物流方式 |

接口版本以官方现行文档为准（总纲 §7.1）；实施时发现版本变了，照现行版改并在父 issue 说明。

## 4. 要做的事

1. 每店一个客户端实例（`Client-Id` + `Api-Key` 由调用方传入），所有请求经 S1-A 的限流器：全局每 Client-Id 50 次/秒 + 单接口限额；429 按 `Retry-After` 等。
2. 实现上表 8 个方法；返回具体类型，不在本包定义「给别人 mock 用」的接口。
3. 测试：每个方法用 httptest + 录制响应单测；真实店冒烟用带 `smoke` 构建标签的测试，默认不跑，只调只读方法。
4. **开工实测**（结论贴 S1 父 issue）：
   - 用真实店确认官方物流「发运单」对应的接口（总纲 §7.4【未验】）；
   - 有本土店的话，摸底它与跨境店的接口差异（总纲 §7.1【未验】）。
   - 结论若要改总纲，另走「改旧 spec」卡，不在本份直接改总纲。

## 5. 验收

- [ ] 方法清单里每个方法都有基于录制响应的单测，不连真实店也能跑
- [ ] 能解析出 S1-D 要用的字段：`status`、`substatus`、`tpl_integration_type`（`ozon` / `aggregator` / `3pl_tracking` / `non_integrated` / `hybryd`）、`shipment_date`、`parent_posting_number`
- [ ] 模拟 429 + `Retry-After`，经限流器等待后重试成功
- [ ] 真实店冒烟：`ListPostings`、`GetPosting`、`GetRoles` 成功
- [ ] 两项开工实测结论已贴父 issue

## 6. 机制清单

无新增机制。落地总纲 §5.5 限流重试（Ozon 侧）与 §5.8 凭据到期告警（读到期时间部分）。

## 7. 自定细节

- 方法命名如第 3 节表。
- 录制响应放 `internal/ozon/testdata/`；冒烟测试构建标签 `smoke`。
- 写操作（备货、面单、传单号）不做真实店冒烟，放到 S1 父 issue 的端到端联调里验。

## 8. 审核修订记录

PR #4 重审（owner 2026-10-07 点选「改」）的处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 4-13 | 轻微 | FBP 拉单无波次归属 | 改：归 S2（总纲 §12.2 / §9·S2 / §13.1 同步）；本份「不管」列明 FBP 拉单属后续波次 |

PR #14 实施期修正（owner 2026-10-07 点选「改旧 spec」）：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 5-1 | 低 | 混合方案拼写 `hybrid` 与官方原文 `hybryd` 不符（实施对照 swagger 发现） | 改：§5 验收按官方原文改 `hybryd`；同步总纲 §7.4、S1-D §4 |
| 5-2 | 低 | §3 表格「多包裹用 `/ship/package`」与官方现行语义不符（`/ship/package` 是「部分组装」；多包裹由 `/ship` 的 `packages` 数组支持） | 改：§3 表格改为「多包裹用请求里的 `packages` 数组」；实现按 `/ship`（未实现 `/ship/package`，S1 场景不需） |
