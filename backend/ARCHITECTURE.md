# 项目结构规范（ARCHITECTURE.md）

> 由 hi-backend init 生成于 2026-10-07（Go 标准档 + 本项目约定）。audit 以本文件为准。
> 细则见 hi-backend tier-standard；**与本「本项目约定」冲突处，一律以本项目约定为准**
> （依据：[ADR-20261007-go-package-deps-v2](../docs/decisions/2026-10-07-go-package-deps-v2.md)、
> [ADR-20261007-backend-stack](../docs/decisions/2026-10-07-backend-stack.md)）。

## 项目概况

- 语言/框架：Go 1.27 + Gin + GORM + slog + Viper + asynq（worker 与 HTTP 同进程）
- 构建命令：`go build ./...`
- 测试命令：`go test ./...`；集成测试（真 MySQL + 真 Redis）：
  `TEST_MYSQL_DSN='root:...@tcp(127.0.0.1:3306)/ozon_dropship_test?parseTime=true&loc=UTC' TEST_REDIS_ADDR=127.0.0.1:6379 go test -tags integration -p 1 ./...`
- 代码检查：`golangci-lint run ./...`
- 迁移：`goose -dir migrations mysql "$DSN" up|down`（真配置见 `config/config.example.yaml`）

## 结构隐喻

按业务分包：`internal/` 下每个业务一个域目录，横切基础设施走 `internal/infra/`，无 service 层。

## 目录职责表

| 目录 | 职责 | 备注 |
|---|---|---|
| `cmd/api/` | 主进程：HTTP 服务 + asynq worker 同进程，只做装配 | 组合根，手写 DI |
| `cmd/createuser/` | 建操作台用户（首个管理员） | 管理 CLI |
| `cmd/genvaultkey/` | 生成保险箱主密钥（部署一次性） | 管理 CLI |
| `config/` | `config.example.yaml`；真 `config.yaml` 不进 git | |
| `internal/<域>/` | 业务域：handler + 业务逻辑 + DTO + 数据读写 | 现有：`store`（店铺与凭据）、`auth`（用户与会话） |
| `internal/infra/` | 横切基础设施：config / db / logger / queue / ratelimit / vault / audit / notify / snowflake / utils | 不依赖任何域 |
| `internal/middleware/` | 会话、角色、恢复、请求日志（RID）、写操作审计 | |
| `internal/router/` | 唯一路由注册，把各域串起来 | 中间件顺序固定 |
| `internal/testutil/` | 集成测试助手（真库 / 真 Redis / 真保险箱） | 测试支撑包，生产二进制不 import |
| `migrations/` | goose 手写 SQL 迁移（时间戳编号） | 生产不用 GORM 自动迁移 |

## 依赖方向规则

- 域 → `infra/` / `middleware/`；禁止复活 `internal/service/`；无 `pkg/`。
- **业务包之间可直接 import，但只许单向、禁循环**（覆盖标准档「域之间禁止横向 import」）：
  `infra/*` ← `ozon`、`alibaba` ← 业务包；业务包之间 `store`、`catalog` 在下（上方各包都可用），
  `order` 依赖 `store`；`purchase` 依赖 `order`、`catalog`、`store`；
  `shipment` 依赖 `order`、`purchase`、`store`（`purchase` 这条是 2026-10-07 新增，
  用途：交接对照表要 `purchase_orders.domestic_tracking_no`；只走一条只读方法，不写 purchase 的表）。
  反方向的触发（如新订单要生成采购任务）走 asynq 任务，不反向 import。
- **接口放使用方**：只在真需要时（反向调用、并行开发、替换实现）由使用方的包定义小接口；
  实现方返回具体类型；不为 mock 预先在实现方定义接口。
- 外部接口客户端放 `internal/ozon`、`internal/alibaba`，包名即用途。

## 配置管理

- 配置来源：Viper + `config/config.yaml`；顶层 `server` 节全局共享，`server.mode`（release/debug）
  决定加载哪个 `database` 分节；组件段（`redis` / `vault` / `session` / `notify` / `ratelimit` / `queue`）
  可选——缺省静默不启用或落默认值，服务照常启动。
- 密钥存放：`config/config.yaml` 进 `.gitignore`，只提交 `config/config.example.yaml`；
  凭据保险箱主密钥由 **systemd `LoadCredentialEncrypted`** 注入到 `$CREDENTIALS_DIRECTORY`，
  **不放环境变量、不进 git**。

## API 约定

- 统一返回格式：`{"code":0,"message":"ok","data":...,"error":...}`；`code=0` 成功、非 0 失败，
  **业务错误亦 HTTP 200**；只有 401（未登录）/ 403（角色不够）/ 429（限流）/ 5xx（真崩了）用非 200。
  统一走 `infra/utils` 的 `SuccessResp / FailResp / FailWithCode / ValidateError / Unauthorized /
  Forbidden / ServerError`，handler 不直接 `c.JSON`（`/health` 例外）。
- 错误处理：哨兵 error + `errors.Is`，业务不 panic。
- 日志：`log/slog` JSON 到 stdout；`internal` 禁 `fmt.Print` 系；每请求带 RID（响应头 `X-Request-ID`）。

## 依赖管理约定

- 锁文件：`go.sum`。
- 引入新依赖：同类只留一个（例：会话存储复用 go-redis 实现 30 行 `scs.Store`，不为 scs 另引 redigo）。
- asynq 仍是 0.x：版本锁在 `go.mod`，升级单独评审。

## 检查参数（audit 用）

- 巨型文件阈值：500 行
- 重复代码阈值：20 行 × 2 处
- 豁免清单：无

## 本项目约定（覆盖标准档预填，依据生效 ADR）

1. **包依赖**：见「依赖方向规则」——业务包可单向 import、接口放使用方、客户端放
   `internal/ozon|alibaba`。（ADR-20261007-go-package-deps-v2）
2. **登录**：scs 会话 cookie，会话存 Redis，**不用 JWT**。（ADR-20261007-backend-stack）
3. **迁移**：goose 手写 SQL，**生产不用 GORM AutoMigrate**。（同上）
4. **模型与表**：表名复数、与总纲 §6 一致，用 `TableName()` 显式声明；主键 = 雪花 ID 字符串；
   业务表带 `del_flag` 软删；`audit_logs` 只追加（无 `del_flag` / `updated_at`）；时间按 UTC 存
   （GORM `NowFunc` 统一 UTC；DSN `loc=UTC`）。
5. **CORS**：不启用（生产由 Caddy 同源反代 `/api`；本地联调走前端 dev 代理）。
6. **日志落盘**：只写 stdout JSON，不落文件、不做轮转（systemd journal 收集）。
7. **外键**：不建，引用一致性由应用层保证（迁移里只有索引与唯一键）。

## 修订记录

- 2026-10-07：init 初版（Go 标准档 + 本项目约定：包依赖 / 登录 / 迁移 / 模型表名 / CORS / 日志 / 外键）。
- 2026-10-07：依赖链补 `shipment` → `purchase`（ADR-20261007-go-package-deps-v2；实现见 PR #16）。
