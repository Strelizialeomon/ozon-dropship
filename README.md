# ozon-dropship

Ozon（俄罗斯电商平台）店铺订单与中国货源平台（1688 等）的对接项目：订单同步、代发采购、发货物流、退货处理。

**当前状态**：需求已确认，总 spec 已定稿（v1.3）；S1 已拆 6 份子 spec（见总纲 §12.3），按波次实施中——S1-A 后端地基实施中（issue #6），B/C/D/E/F 等 A 合并后开工。

## 从这里看起

- [总 spec：履约中台设计](docs/specs/spec-fulfillment-hub.md) —— 需求、架构、机制、数据模型、验收、波次。
- [决策记录（ADR）](docs/decisions/) —— 自研、货源接入、仓库结构、前后端技术栈、部署等长期决定。
- [Issue #1：前因后果 + 调研结论](https://github.com/Strelizialeomon/ozon-dropship/issues/1) —— 项目背景、Ozon / 中国货源 / 物流三侧调研详情、硬约束、待确认问题。

## 仓库布局（两端）

本仓是总仓，前后端两个子项目依赖各自分开（[ADR-20261007-repo-layout](docs/decisions/2026-10-07-repo-layout.md)）：

| 目录 | 是什么 | 怎么跑 |
|---|---|---|
| `backend/` | Go 后端（gin + GORM + MySQL + Redis/asynq，HTTP 与任务 worker 同进程） | `cd backend`，cp `config/config.example.yaml` 为 `config/config.yaml`，`go run ./cmd/api` |
| `frontend/` | React 操作台（S1-E，未开工） | — |
| `deploy/` | Caddy / systemd / 备份（S1-F，未开工） | — |
| `docs/specs/`、`docs/decisions/` | 子 spec 与 ADR | — |

后端目录、依赖方向、API / 配置约定见 [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md)。
