# spec-s1a-foundation —— S1-A 后端地基（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-A**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#6](https://github.com/Strelizialeomon/ozon-dropship/issues/6) ｜ 状态：v1.0（2026-10-07）｜ 发布序：**第 1 批，硬前置**（B、C、D、E 都等它合并后开工；F 随时可开）
> 跨份验收与协作声明：S1 父 issue [#5](https://github.com/Strelizialeomon/ozon-dropship/issues/5)

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| `backend/` 骨架、依赖清单、启动装配、配置 | Ozon / 1688 客户端（S1-B、S1-C） |
| 迁移：S1 全部表 | `order` / `purchase` / `shipment` / `catalog`（S1-D） |
| `internal/infra/`：DB、Redis 与 asynq、限流、凭据保险箱、审计、通知、日志 | 前端（S1-E）、`deploy/`（S1-F） |
| 登录与角色、唯一路由注册 | 推送入口、库存同步（S2） |
| `store` 包：店铺、凭据、到期告警 | 退货、财务的表与逻辑（S3 / S4） |

## 2. 地盘

- **独占**：`backend/go.mod`、`backend/go.sum`、`backend/cmd/**`、`backend/config/**`、`backend/migrations/**`、`backend/internal/infra/**`、`backend/internal/middleware/**`、`backend/internal/router/**`、`backend/internal/store/**`、`backend/ARCHITECTURE.md`、根 `README.md`、根 `AGENTS.md`。
- **不碰**：`backend/internal/{ozon,alibaba}/**`（B、C）、`backend/internal/{order,purchase,shipment,catalog}/**`（D）、`frontend/**`（E）、`deploy/**`（F）。
- **共享件**：本份是 `go.mod`、`cmd/api`、`internal/router`、`migrations` 的主人；合并后别的份要改，先在 S1 父 issue 下声明（总纲 §12.3）。

## 3. 要做的事

1. **骨架**：`go.mod` 预置 S1 全部依赖（后端 ADR 所列各库）；`cmd/api` 按「配置 → 日志 → DB → Redis / asynq → 客户端 → handler → 路由 → HTTP 服务 → 优雅退出」装配；`config/` 用 viper，入库只放 example；用 hi-backend 生成 `ARCHITECTURE.md`，并收编 [ADR-20261007-go-package-deps](../decisions/2026-10-07-go-package-deps.md) 的覆盖规则（标准档预填与生效 ADR 冲突处一律以 ADR 为准并写进「本项目约定」；至少：包依赖、登录、迁移）。
2. **迁移**（goose 手写 SQL）：总纲 §6 中 S1 用到的 13 张表——`stores`、`relay_points`、`credentials`、`supplier_offers`、`offer_links`、`orders`、`order_items`、`purchase_tasks`、`purchase_orders`、`shipments`、`exceptions`、`audit_logs`、`users`；关键字段、唯一键、`DECIMAL(18,4)` + 币种、UTC 时间照 §6。
3. **`internal/infra/`**：
   - `queue`：asynq 服务与定时器同进程；补投扫描框架——各业务包按「表 + 状态 + 超时」注册规则（总纲 §5.5）。
   - `ratelimit`：Ozon 每店 × 每接口令牌桶；1688 为「应用 + 接口」企业级桶（限额未公开【未验】，实施期实测）；429 / 超限读 `Retry-After`（有则）；指数退避，默认上限 3 次。
   - `vault`：Tink 加密，关联数据 = 表名 + 行 ID；主密钥从 systemd 凭据目录读（总纲 §5.4）。
   - `audit`（写 `audit_logs` 的统一入口）、`notify`（飞书机器人：签名、按错误去重、按分钟合并）、`logger`（slog JSON）、`db`、`config`。
4. **登录与角色**：`internal/middleware` 用 scs + Redis 存会话；`admin` / `operator` 两级。
5. **`internal/store`**：店铺增删改查；凭据录入 / 脱敏展示 / 轮换；到期检查任务（总纲 §5.8）。Ozon 自动读到期时间所需的小接口由本包定义，S1-B 合并后在装配层接上。
   - **对外提供凭据读写方法**（跨份契约）：按「凭据种类 + 店铺」读写明文（企业级凭据店铺为空——1688 token 属此类，见总纲 §6），内部经保险箱加解密，并能写入到期时间。S1-C 存 1688 token、S1-D 取 Ozon 凭据都走它；方法签名在本份实施时定下后贴 S1 父 issue，C、D 照此定义各自的小接口。
6. **首个管理员**：`cmd/createuser`。
7. **根文档**：`README.md` 加「两端布局」表；`AGENTS.md` 补一句仓库结构，指向仓库结构 ADR。

**本份提供的操作台接口**（S1-E 照此手写）：

| 路径前缀 | 用途 |
|---|---|
| `/api/auth` | 登录 / 登出 / 当前用户 |
| `/api/stores`、`/api/credentials` | 店铺与凭据（只返回脱敏尾号与剩余天数） |
| `/api/system` | 系统状态：各店最近一次同步成功时间（读 `stores.last_sync_at`，写方 S1-D）、队列积压、失败任务 |
| `/api/admin/queues` | asynq 队列监控页（仅 admin） |

## 4. 验收

- [ ] `go build ./...`、`go test ./...`、golangci-lint 全绿
- [ ] goose 在空库 up / down 跑通；13 张表的关键字段、唯一键与总纲 §6 一致
- [ ] asynq 与 HTTP 同进程启动；杀进程再重启后，停在「待处理」且超时的记录被补投扫描重新入队
- [ ] 限流：同店同接口超额时排队；模拟 429 + `Retry-After` 按其等待；重试 3 次用尽返回明确错误
- [ ] 凭据：库里是密文；密文挪到别的行解不开；接口只返回脱敏尾号；读取与轮换写审计
- [ ] 到期告警：`expires_at` 距今 14 / 7 / 1 天各发一次飞书，同一档不重复
- [ ] 登录：会话在 Redis；踢人后立即失效；operator 调 admin 接口返回 403
- [ ] 所有写接口都写审计
- [ ] `ARCHITECTURE.md` 已收编包依赖 ADR（依赖方向、接口放使用方、客户端目录），且「本项目约定」覆盖项与生效 ADR 对得上（至少：包依赖、登录、迁移）

## 5. 机制清单

无新增机制。落地总纲 §5.4 凭据保险箱、§5.5 调度 + 限流重试（框架部分）、§5.8 凭据到期告警、§5.11 审计留痕。

## 6. 自定细节

- Go 用当前稳定版 1.27；module 路径 `github.com/Strelizialeomon/ozon-dropship/backend`。
- 入口叫 `cmd/api`（hi-backend 蓝图命名）；首个管理员用 `cmd/createuser`（照 xhs-analysis）。
- 迁移目录 `backend/migrations/`，goose 时间戳编号。
- 到期检查每天北京时间 09:00 跑一次；「站内提醒」= 系统状态页实时算剩余天数，不落表。
- 补投扫描默认每 5 分钟（总纲 §13.1）。
- 会话用 scs（存 Redis）；日志用标准库 slog、JSON 输出。
- 飞书通知：同错误去重、按分钟合并。

## 7. 审核修订记录

PR #4 重审（owner 2026-10-07 点选「改」）的处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 3-3 | 轻微 | 头部括注「B、C、D、F 都等它合并」与发布序不符 | 改：改为「B、C、D、E 都等它合并后开工；F 随时可开」 |
| 3-5 | 轻微 | 独占范围含根 README / AGENTS，总纲 §12.3 未列 | 改：总纲 §12.3 A 行已补齐 |
| 3-7 | 轻微 | 自定细节缺会话 / 日志 / 通知三项 | 改：已补进 §6 |
| 4-1 | 严重 | 异常池无存储载体 | 改：总纲新增 `exceptions` 表；本份迁移 12→13 张 |
| 4-2 | 中 | 企业级凭据的店铺维度未定义 | 改：契约按总纲 §6「店铺可空」口径写明 |
| 4-3 | 中 | 同步时间无存储无写方 | 改：`/api/system` 注明读 `stores.last_sync_at`（S1-D 写） |
| 4-4 | 中 | ARCHITECTURE.md 覆盖口径只写一条 | 改：生成与验收同步 ADR 覆盖规则 |
| 4-6 | 轻微 | 1688 限流桶形状先于限额冻结 | 改：定「应用级桶」+ 实测项 |
