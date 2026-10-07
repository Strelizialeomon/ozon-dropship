# ADR-20261007-go-package-deps-v2：包依赖链补一条 `shipment` → `purchase`（取代 v1）

- 状态：生效中
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：`backend/` 全部 Go 包的依赖关系与目录命名
- 取代：[ADR-20261007-go-package-deps](2026-10-07-go-package-deps.md)
- 被取代：无
- 决策来源：2026-10-07 PR #16（S1-D 履约编排）合并闸卡当场拍板——owner 点选「接受这条边（推荐）」

## 决定

**v1 的三条原则原样沿用**（业务包可直接 import、只许单向、禁循环；接口放使用方；外部客户端放
`internal/ozon` / `internal/alibaba`），本版只改**依赖链**这一处：

1. 依赖链（原：`catalog`、`store` 在下；`order` 依赖 `store`；`purchase` 依赖 `order`、`catalog`；
   `shipment` 依赖 `order`）**新增一条边**：

   - `store`、`catalog` 在下；`order` 依赖 `store`；
   - `purchase` 依赖 `order`、`catalog`；
   - **`shipment` 依赖 `order`、`purchase`**；
   - `internal/infra/*` ← `internal/ozon`、`internal/alibaba` ← 业务包；下层不 import 上层。
   - 新增业务包按同样原则排进这条链；反方向的触发走 asynq 任务，不反向 import。

2. **新增这条边的用途与收窄口径**：总纲 §5.7 的货代仓交接对照表要「国内快递号 ↔ posting_number ↔
   面单」，国内快递号落在 `purchase_orders.domestic_tracking_no`。`shipment` 对 `purchase` 的调用
   **只走一条读接口**（`purchase.Repo.DomesticTrackingByOrder`）：不写 purchase 的表、不碰它的状态机。

## 依据

- 与 v1 同一组 Go 官方依据（[Import declarations](https://go.dev/ref/spec#Import_declarations)、
  [Organizing a Go module](https://go.dev/doc/modules/layout)、
  [Code Review Comments · Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)）——
  改动只多一条单向边，三条原则不变，不构成环。
- 仓库里已落护栏：`backend/internal/order/deps_test.go` 解析源码把允许的边写死，
  谁加反方向 import 当场红（比等编译器报环更早）。

## 否决了什么

- **`shipment` 直接读 `purchase_orders` 表**（绕过包边界：以后 purchase 改表没人知道）。
- **让 `purchase` 把国内快递号回写 `shipments` 表**（跨包写 + 要给 shipments 加列 = 改迁移与总纲 §6，最重）。
- v1 否决过的选项照旧否决（见被取代的那条）。

## 接受的代价

- 依赖链多一条边：`shipment` 的构建与测试会连带 `purchase`；哪天交接导出不再需要采购域字段，这条边要撤掉。
- 其余代价沿用 v1（与 hi-backend 默认规则不一致、单向依赖靠人守 + 护栏测试兜）。

## 允许重开的条件

- 依赖链出现难以拆解的双向需求、频繁要靠异步任务或接口绕行；
- 业务包规模大到需要升到更重的分层档位；
- 交接对照表不再需要采购域的字段（那时应撤掉这条新增的边）。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4、§5.7、§12.3
- [spec-s1d-fulfillment](../specs/spec-s1d-fulfillment.md) §2
- PR #16（本决定的来源）
