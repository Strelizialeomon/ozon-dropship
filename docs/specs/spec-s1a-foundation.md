# spec-s1a-foundation —— S1-A 后端地基（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-A**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#6](https://github.com/Strelizialeomon/ozon-dropship/issues/6) ｜ 状态：v1.1（2026-10-07：PR #13 重审处置——新增「登录失败限速」机制、自定细节补参数，见 §7 第 2 次）｜ 发布序：**第 1 批，硬前置**（B、C、D、E 都等它合并后开工；F 随时可开）
> 跨份验收与协作声明：S1 父 issue [#5](https://github.com/Strelizialeomon/ozon-dropship/issues/5)
> 实施状态：已合并（PR #13），待上线

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

新增 1 个机制（其余为落地总纲既有机制）：

| # | 机制 | 解决什么 / 没有它会怎样 | 批准 |
|---|---|---|---|
| A-1 | 登录失败限速 | 登录在公网上可被无限次尝试爆破；没有它，弱口令 + 无痕爆破没有刹车 | owner 2026-10-07（PR #13 重审修订卡点选「全改含登录限速」） |

落地总纲 §5.4 凭据保险箱、§5.5 调度 + 限流重试（框架部分）、§5.8 凭据到期告警、§5.11 审计留痕。

## 6. 自定细节

- Go 用当前稳定版 1.27；module 路径 `github.com/Strelizialeomon/ozon-dropship/backend`。
- 入口叫 `cmd/api`（hi-backend 蓝图命名）；首个管理员用 `cmd/createuser`（照 xhs-analysis）。
- 迁移目录 `backend/migrations/`，goose 时间戳编号。
- 到期检查每天北京时间 09:00 跑一次；「站内提醒」= 系统状态页实时算剩余天数，不落表。
- 补投扫描默认每 5 分钟（总纲 §13.1）。
- 会话用 scs（存 Redis）；日志用标准库 slog、JSON 输出。
- 飞书通知：同错误去重、按分钟合并。
- 登录失败限速：同一「IP + 用户名」10 分钟内失败 10 次即 429（进程内计数、成功清零；参数为 auth 包常量，见 §5 A-1）。
- 凭据 Put 的 `expires_at` 语义：不传 = 保留原到期时间（防止轮换换新 key 时把到期时间误抹成空、从此不再告警）。

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

**第 2 次：重审（owner 2026-10-07 在 PR #13 点选「重审」，基准 issue-6 HEAD）**，原文见 PR #13 评论。两路：规格符合性（路一，7 条 = 中 1、轻微 6）、对抗式找 bug（路二，14 条 = 严重 1、中 7、轻微 6）。owner 点选「全改含登录限速」，全部处置如下（v1.1；表内 1-x = 路一发现，2-x = 路二发现）：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 1-1 | 中 | go.mod 未预置 shopspring/decimal（子 spec §3.1 要求） | 改：go get v1.5.0 + tools.go 钉成直接依赖 |
| 1-2 | 轻微 | internal/auth、internal/testutil 为总纲 §4 包清单外的包 | 不改：落点理由已贴父单 #5 待 owner 过目；ARCHITECTURE.md 已记录 |
| 1-3 | 轻微 | cmd/genvaultkey、infra/snowflake、infra/utils 为清单外新增 | 不改：均服务 §4 验收（密钥生成、雪花 ID、响应壳），ARCHITECTURE.md 已记录 |
| 1-4 | 轻微 | credentials 多出 masked_tail / expiry_alert_stage / 生成列；stores、relay_points 唯一键 | 不改：为 §4 验收所需（脱敏尾号、同档不重复、软删后重建），迁移内有注释 |
| 1-5 | 轻微 | 清单外默认数值（1688 5 rps 占位、退避上限 5 分钟、去重 60 分钟、会话 7 天） | 不改：均为配置化默认值；1688 值待 S1-C 实测回填 |
| 1-6 | 轻微 | DSN 未钉 MySQL 会话时区 | 改：DSN 加 `time_zone='+00:00'`（与 2-13 同一处） |
| 1-7 | 轻微 | 剩余天数在 /api/credentials 的 days_left、不在 /api/system | 不改：按 §3 接口表口径即如此（站内提醒=凭据页实时算） |
| 2-1 | 严重 | 补投扫描对已归档任务永久失效（ErrTaskIDConflict 被无条件当成功） | 改：撞冲突时查任务状态，死记录（archived/completed）清除后重投；回归测试 `TestEnqueueRevivesArchivedTask` |
| 2-2 | 中 | 审计可被超长输入整条抹掉（MySQL 1406） | 改：audit.Record 按列宽字符级截断（含省略号不超长） |
| 2-3 | 中 | 写接口 panic → 审计跳过 | 改：AuditWrite 改 defer 记录并标注 `panicked`，panic 继续上抛给 Recovery |
| 2-4 | 中 | 畸形登录请求不写审计 | 改：绑定失败分支也记 `auth.login.fail`（bad_request） |
| 2-5 | 中 | 同店同 kind 并发 Put → 500 且本次轮换未生效 | 改：撞唯一键后转更新路径（create → applyRotation） |
| 2-6 | 中 | 软删行继续占用唯一键（同名店/同 kind 凭据再也建不出） | 改：5 组唯一键改为「仅存活行唯一」（del_flag 条件生成列） |
| 2-7 | 中 | 队列重试退避差一位（首次重试等 0 秒） | 改：`retryDelay = base × 2^n` 并 clamp（n<0 归 0、n>20 封顶），单测钉住 |
| 2-8 | 中 | 总闸令牌桶 burst=50 → 1 秒窗口可达 ~100 次 | 改：总闸桶 burst 固定 1（严格匀速） |
| 2-9 | 轻微 | 飞书失败（HTTP 200 + code≠0）被当成功；失败即丢消息 | 改：解析 body 的 code；发送失败把内容放回缓冲、下轮重试 |
| 2-10 | 轻微 | 轮换不传 expires_at 会把原到期时间抹成 NULL | 改：nil = 保留原值（显式传值才更新并复位告警档），已写入 §6 |
| 2-11 | 轻微 | Shutdown 并发调用可能跳过真正停机 | 改：整个停机流程进一个 sync.Once |
| 2-12 | 轻微 | Save 会把软删店写活（TOCTOU） | 改：Update 改为带 `del_flag = false` 条件的字段更新，0 行返回 ErrShopNotFound |
| 2-13 | 轻微 | MySQL 会话时区未钉，DDL 默认值可能落服务器本地时区 | 改：同 1-6 |
| 2-14 | 轻微 | 登录无失败限速 | 改：新增机制 A-1（§5）：IP + 用户名，10 次 / 10 分钟 |
