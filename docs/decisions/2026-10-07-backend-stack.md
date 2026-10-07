# ADR-20261007-backend-stack：后端用 Go + gin + GORM + MySQL 8.4 + Redis/asynq

- 状态：生效中
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：`backend/` 全部代码
- 取代：无
- 被取代：无
- 决策来源：spec v1.0 头部（Go + GORM + MySQL 拍板）；2026-10-07 会话（owner 点名 gin + Redis + asynq；逐项选择卡拍板其余各项）

## 决定

| 部件 | 选型 |
|---|---|
| 语言 / Web 框架 / ORM | Go、gin、GORM |
| 数据库 | MySQL 8.4 长期支持版 |
| 后台任务 / 定时任务 | Redis + asynq，worker 与 HTTP 服务同进程 |
| 表结构迁移 | goose，手写 SQL（生产不用 GORM 自动迁移） |
| 金额 | shopspring/decimal + 数据库 DECIMAL，币种单独一列 |
| 字段加密 | Tink-go；主密钥由 systemd 加密凭据注入 |
| 登录 | session cookie，会话存 Redis |
| 日志 / 告警 | log/slog + 飞书机器人 + asynq 队列监控页 |
| Ozon / 1688 接口客户端 | 自己写，只写用到的接口 |
| 接口对接 | 不出接口文档、不用代码生成（不用 Swagger / swag / OpenAPI / orval）；接口以 Go 代码为准 |

## 依据

- 前后端同一人维护，接口文档与代码生成的收益抵不过维护成本。
- MySQL 8.0 已于 2026-04 停止支持；GORM 官方建议生产用版本化迁移。
- Ozon 社区 Go SDK 最新发版 2025-03、最后提交 2025-10，且缺发运单 / 推送配置 / 密钥角色等接口；1688 官方 SDK 只有 Java / PHP / .Net / Python，没有 Go。

## 否决了什么

- huma + 标准库路由（推荐过，owner 选 gin）；自建 MySQL 任务表（owner 选 Redis + asynq）。
- swag / Swagger 文档、orval 前端代码生成。
- GORM 自动迁移上生产；整数存「分」；JWT 登录；Prometheus + Grafana；直接用社区 Ozon / 1688 SDK。

## 接受的代价

- 多一个 Redis 组件：必须开 AOF，并靠补投扫描兜住「写库与入队不在同一事务」。
- asynq 仍是 0.x，接口可能不兼容地变 → 锁定版本号。
- 没有接口文档，前端类型手写，后端改字段要人记得同步前端。
- goose 迁移要手写 SQL。

## 允许重开的条件

- asynq 停止维护，或因 Redis 丢任务出过实际事故；
- 单机性能撑不住业务量；
- 维护人数增加、前后端分人，需要接口契约。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4.1、§5.4、§5.5、§6
