# ozon-dropship

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)

> **English**: A self-hosted fulfillment hub for Ozon (Russian marketplace) sellers sourcing from China (1688 and similar): it syncs orders, places dropship purchases, tracks shipping, and sends tracking numbers back to Ozon. Open source under AGPL-3.0.

**一套自托管的 Ozon 履约中台**：Ozon 店铺来单后自动接住，替你去中国货源平台下单代发，盯着中转仓收货，再把国际快递单号回传回 Ozon——把「一单一单手工搬」的履约流程交给程序。为补上现成 ERP 在采购下单环节的自动化缺口而写。**已开源**（[AGPL-3.0](#许可)）。

## 能干什么

- **订单同步**：定时从 Ozon 拉单；推送式同步开发中
- **采购任务**：把 Ozon 订单翻译成「去 1688 买什么、寄到哪」——人工轨道（人下单、回填单号）与自动轨道（程序下单）双轨，自动轨道开发中
- **中转与交运**：货到中转仓点收、打包、贴面单、交干线，全程状态可查
- **单号回传**：国际快递单号自动回填 Ozon
- **库存同步**：货源断货自动把对应 Ozon 链接的库存清零（开发中）
- **异常池**：超时、下单失败、状态不认识……自动进池等人处理，不静默失败
- **操作台**：7 个页面的网页界面（订单 / 采购 / 中转 / 映射 / 异常 / 店铺凭据 / 系统状态）

## 项目状态

早期开发中：核心闭环（订单 → 采购 → 中转 → 回传）已实现，**尚未生产部署**；接口与数据结构可能变动。

## 部署

正式部署需要：一台 Linux 服务器 + 域名 + MySQL 8.x + Redis 7.x，以及各平台（Ozon / 1688）的自有凭据与相应权限。步骤照 [deploy/install.md](deploy/install.md)。

## 开发者：本地跑起来

前提：Go 1.27+、[Bun](https://bun.sh)、MySQL 8.x、Redis 7.x。完整开发说明（测试 / 风格 / 提交习惯）见 [CONTRIBUTING.md](CONTRIBUTING.md)。

```bash
# 后端（需本地 MySQL / Redis 已起，建表见 CONTRIBUTING.md）
cd backend
cp config/config.example.yaml config/config.yaml   # 按本机改数据库口令等
go run ./cmd/api

# 前端操作台（另一个终端）
cd frontend
bun install && bun run dev
```

## 文档地图

| 想做什么 | 从哪看起 |
|---|---|
| 看设计 / 改代码 | [总纲](docs/specs/spec-fulfillment-hub.md)（设计权威）→ [决策记录（ADR）](docs/decisions/) → [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md) |
| 部署 / 运维 | [deploy/install.md](deploy/install.md)（首次安装）、[deploy/release.md](deploy/release.md)（日常发布与回滚）、[deploy/backup/restore-drill.md](deploy/backup/restore-drill.md)（恢复演练） |
| 操作与上线 | [docs/handover.md](docs/handover.md)——操作与上线手册：术语表 / 上线步骤 / 日常使用 / 故障处理 |

目录速查：`backend/`（Go 后端）、`frontend/`（React 操作台）、`deploy/`（部署与备份脚本）、`docs/`（spec 与决策记录）。前后端依赖各自分开，见 [ADR-20261007-repo-layout](docs/decisions/2026-10-07-repo-layout.md)。

## 参与

欢迎 issue 和 PR——动手前先读 [CONTRIBUTING.md](CONTRIBUTING.md)（环境、测试、提交习惯）；**安全问题不要开公开 issue**，走 [SECURITY.md](SECURITY.md) 的私密通道。

## 开发流程

- **需求 → 设计 → 实施**：先出 spec 放 `docs/specs/`，再开 issue 实施。
- **长期决定**写进[决策记录（ADR）](docs/decisions/)：一条决定一个文件，定了就不再反复重开。
- **每份交付物合并前过一道审核**；每个 issue 挂状态标签，卡在哪看标签。

细则在 [AGENTS.md](AGENTS.md)，这里不复述。

## 许可

[AGPL-3.0](LICENSE) © 2026 Strelizialeomon

你可以自由使用、修改、分发本项目；但**改过的版本必须同样以 AGPL-3.0 开源**——包括把它作为网络服务提供给别人的场景（AGPL 第 13 条）。
