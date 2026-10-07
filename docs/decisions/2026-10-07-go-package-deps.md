# ADR-20261007-go-package-deps：Go 包依赖按官方规矩——可直接 import、只许单向、禁循环；接口放使用方

- 状态：已被取代
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：`backend/` 全部 Go 包的依赖关系与目录命名
- 取代：无
- 被取代：[ADR-20261007-go-package-deps-v2](2026-10-07-go-package-deps-v2.md)
- 决策来源：2026-10-07 会话（owner：hi-backend「业务域之间禁止互相 import」不以为准，要求按 Go 官方；选择卡点选「单向、禁循环」与「`internal/ozon`、`internal/alibaba`」）

## 决定

1. 业务包之间**可以直接 import**，但依赖方向必须单向、**禁止循环**：
   - `internal/infra/*` ← `internal/ozon`、`internal/alibaba` ← 业务包；下层不 import 上层。
   - 业务包之间：`store`、`catalog` 在下；`order` 依赖 `store`；`purchase` 依赖 `order`、`catalog`；`shipment` 依赖 `order`。新增业务包按同样原则排进这条链。
   - 反方向的触发（如新订单要生成采购任务）走 asynq 任务，不反向 import。
2. **接口放使用方**：只在真有需要时（反向调用、并行开发、替换实现）由使用方的包定义小接口；实现方返回具体类型；不为 mock 预先在实现方定义接口。
3. 外部接口客户端放 `internal/ozon`、`internal/alibaba`，包名即用途。
4. 目录骨架其余部分仍照 hi-backend 标准档（`cmd/`、`internal/<域>/`、`internal/infra/`、`internal/middleware/`、`internal/router/`、不建 `pkg/`）。生成 `backend/ARCHITECTURE.md` 时：本条写成项目约定，覆盖标准档「域之间不许互相 import」条款；**标准档预填与生效 ADR 冲突的其余条款同样以 ADR 为准**，一并写进「本项目约定」（目前已知：登录——标准档 JWT，本项目用会话 cookie；迁移——标准档 GORM AutoMigrate，本项目用 goose 手写迁移；两者见 [ADR-20261007-backend-stack](2026-10-07-backend-stack.md)），未冲突条款照标准档预填。

## 依据（Go 官方）

- 语言规范：「包直接或间接 import 自己是非法的」——循环是唯一的硬性禁止。[Import declarations](https://go.dev/ref/spec#Import_declarations)
- 官方模块布局：服务端项目把实现逻辑的包放 `internal/`、命令放 `cmd/`；未限制 `internal/` 下的包互相引用。[Organizing a Go module](https://go.dev/doc/modules/layout)
- `internal/` 只限制外部模块引用。[cmd/go · Internal packages](https://pkg.go.dev/cmd/go#hdr-Internal_packages)
- 接口一般属于使用该接口的包；实现方返回具体类型；不要在实现方「为 mock」定义接口，也不要在用到之前定义。[Code Review Comments · Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- 包按用途命名，避免 util / common / 统一接口包。[Package names](https://go.dev/blog/package-names)

## 否决了什么

- hi-backend 标准档的「域之间不许互相 import；域只依赖 `internal/infra/` 与 `internal/middleware/`」（标准档的跨域共享出路是「横切下沉 infra、业务合并域或升重档」）；本项目改为可直接 import、只守单向与禁循环。标准档「接口在消费方定义」一条与本 ADR 一致，沿用。
- 把 Ozon / 1688 客户端放进 `internal/infra/`。

## 接受的代价

- 与 hi-backend 默认规则不一致：用 hi-backend 审代码时，要以项目 `ARCHITECTURE.md` 里收编的约定为准。
- 单向依赖靠人守：新增包或新调用要先排进依赖链；循环由编译器兜底报错。

## 允许重开的条件

- 依赖链出现难以拆解的双向需求、频繁要靠异步任务或接口绕行；
- 业务包规模大到需要升到更重的分层档位。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4、§12.3、§13.1
