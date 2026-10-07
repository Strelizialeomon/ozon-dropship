# ADR-20261007-go-package-deps-v2：包依赖链补一条 `shipment` → `purchase`（取代 v1）

- 状态：生效中
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：`backend/` 全部 Go 包的依赖关系与目录命名
- 取代：[ADR-20261007-go-package-deps](2026-10-07-go-package-deps.md)
- 被取代：无
- 决策来源：2026-10-07 PR #16（S1-D 履约编排）合并闸卡当场拍板——owner 点选「接受这条边（推荐）」，结论留痕见该 PR 评论

## 决定

**v1 的决定 1–4 条原样沿用**——业务包之间可直接 import、只许单向、禁循环；接口放使用方；
外部接口客户端放 `internal/ozon` / `internal/alibaba`；目录骨架照 hi-backend 标准档、
生成 `backend/ARCHITECTURE.md` 时把与标准档冲突的条款写进「本项目约定」（登录用 cookie、迁移用 goose 等）。
本版**只改依赖链**这一处：

1. 依赖链（v1 的链 + 一条新边）：

   - `internal/infra/*` ← `internal/ozon`、`internal/alibaba` ← 业务包；下层不 import 上层。
   - `store`、`catalog` 在最下层：**上方各业务包都可以 import 它们**（不逐条列边）；
   - `order` 依赖 `store`；
   - `purchase` 依赖 `order`、`catalog`、`store`；
   - **`shipment` 依赖 `order`、`purchase`、`store`** —— `purchase` 这条是本次新增；
   - `internal/middleware` 横切，谁都可用；新增业务包按同样原则排进这条链；
     反方向的触发（如新订单要生成采购任务）走 asynq 任务，不反向 import。

2. **新增这条边的用途与收窄口径**：总纲 §5.7 的货代仓交接对照表要「国内快递号 ↔ posting_number ↔
   面单」，国内快递号落在 `purchase_orders.domestic_tracking_no`。`shipment` 对 `purchase` 的调用
   **只走一条只读方法**（具体签名以实现为准，见 PR #16）：不写 purchase 的表、不碰它的状态机。

> 落地时点：这条边的实现与护栏测试随 PR #16 落地（写入本 ADR 时该 PR 尚未合并——按 S1-D 发布序
> 等 B、C 之后才合并）。**ADR 记录的是决定，不是现状**：决定自拍板起生效。

## 依据

- Go 官方（与 v1 同一组）：
  - 语言规范：[Import declarations](https://go.dev/ref/spec#Import_declarations)——循环是唯一的硬性禁止；
  - 官方模块布局：[Organizing a Go module](https://go.dev/doc/modules/layout)；
  - `internal/` 只限制外部模块引用：[cmd/go · Internal packages](https://pkg.go.dev/cmd/go#hdr-Internal_packages)；
  - 接口归使用方：[Code Review Comments · Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)；
  - 包命名：[Package names](https://go.dev/blog/package-names)。
- 仓库内护栏：`backend/internal/order/deps_test.go` 解析源码、把允许的边写死（谁加反方向 import 当场红），
  随 PR #16 落地；合入前该文件不在 main 上。

## 否决了什么

- **`shipment` 直接读 `purchase_orders` 表**（绕过包边界：以后 purchase 改表没人知道）。
- **让 `purchase` 把国内快递号回写 `shipments` 表**（跨包写 + 要给 shipments 加列 = 改迁移与总纲 §6，最重）。
- v1 否决过的选项照旧否决（见被取代的那条）。

## 接受的代价

- 依赖链多一条边：`shipment` 的构建与测试会连带 `purchase`；哪天交接导出不再需要采购域字段，这条边要撤掉。
- 「只走一条只读方法」靠约定与评审守（`deps_test.go` 只钉 import 方向，钉不住调用了哪些方法）。
- 其余代价沿用 v1（与 hi-backend 默认规则不一致、单向依赖靠人守 + 护栏测试兜）。

## 允许重开的条件

出现下列任一**新事实**时才重开；重开由 owner 拍板，agent 不得据本条自行改链：

- 依赖链上出现新的**双向需求**（如 purchase 反过来也要读 shipment 的字段）——那时要重新设计，不是再加一条边；
- 新增业务包让链上出现两条以上互相依赖的边；
- 交接对照表不再需要采购域的字段（那时撤掉本条新增的边）；
- 业务包规模大到需要升到更重的分层档位（如按域分层）。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4、§5.7、§12.3
  （⚠️ §4 的 mermaid 箭头画的是**数据流**：订单 → 采购 → 发货；那不是包依赖边，别照它推依赖方向）
- [spec-s1d-fulfillment](../specs/spec-s1d-fulfillment.md) §2
- PR #16（本决定的来源；实现与护栏测试随它落地）
