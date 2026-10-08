# 决策记录（ADR）· 规则页

> 本仓**已启用 ADR**（2026-10-06 用户拍板）。本页是本制度的**唯一规则页**；
> `docs/decisions/` 下的一条条 ADR 是已落 ADR 的决策的唯一入口。
>
> **完整口径不在本页复述**——什么时候写、唯一来源、取代流程、升级到重型机制的条件，
> 见 [ADR 备忘录](https://github.com/Strelizialeomon/spec-flow/blob/main/references/adr-decision-records.md)。本页只留本仓落点。

## 载体分工

| 载体 | 回答什么 |
|---|---|
| `docs/specs/` | 某次需求在当时应当怎么实施 |
| issue | 现在要做什么、谁在做、怎么验收 |
| **`docs/decisions/`（本目录）** | **长期决策定了什么、为什么、何时才能重开** |
| 代码 / 实测记录 | 客观上现在是什么 |

**ADR 不取代 spec；同一条决策上 ADR 与 spec 正文不一致时，以 ADR 为准。**

## 一览

一条一行：链接 + 状态 + 一句话。**状态与各文件头部一致**；已被取代的照旧列出、标「已被取代」，方便追溯。

| ADR | 状态 | 定了什么 |
|---|---|---|
| [ADR-20261007-build-in-house](2026-10-07-build-in-house.md) | 生效中 | 直接自研履约中台，不以现成 ERP 起步 |
| [ADR-20261007-repo-layout](2026-10-07-repo-layout.md) | 生效中 | 一个总仓，`backend/` 与 `frontend/` 两个子项目，依赖分开 |
| [ADR-20261007-backend-stack](2026-10-07-backend-stack.md) | 生效中 | 后端用 Go + gin + GORM + MySQL 8.4 + Redis/asynq |
| [ADR-20261007-frontend-stack](2026-10-07-frontend-stack.md) | 生效中 | 操作台前端用 React + Bun + Rsbuild + 声明式 React Router + jotai + shadcn/ui + Axios |
| [ADR-20261007-sourcing-integration](2026-10-07-sourcing-integration.md) | 生效中 | 1688 走两条自用下单通道；拼多多 / 淘宝人工辅助，不做 RPA |
| [ADR-20261007-deployment](2026-10-07-deployment.md) | 生效中 | 单机部署、不用 Docker；Caddy 管 HTTPS 与前端静态资源，systemd 托管 Go |
| [ADR-20261007-go-package-deps](2026-10-07-go-package-deps.md) | 已被取代 | Go 包依赖按官方规矩：可直接 import、只许单向、禁循环；接口放使用方（由 v2 取代） |
| [ADR-20261007-go-package-deps-v2](2026-10-07-go-package-deps-v2.md) | 生效中 | 在 v1 基础上补一条 `shipment` → `purchase` |
| [ADR-20261007-handover-s2-first](2026-10-07-handover-s2-first.md) | 生效中 | 接手人是非技术 owner；接手后先推 S2 开发，不先上线 |
| [ADR-20261007-open-source](2026-10-07-open-source.md) | 生效中 | 项目以 AGPL-3.0 开源，全仓（含文档与 issue）公开 |
| [ADR-20261008-public-docs-boundary](2026-10-08-public-docs-boundary.md) | 生效中 | 公开仓文档分两层：门面与入口文档按对外口径写，工作文档层保留内部叙述 |

## 本仓落点

- 一条决定一个文件，放本目录；文件名 `YYYY-MM-DD-<slug>.md`，稳定 ID `ADR-YYYYMMDD-<slug>`，状态 `生效中` / `已被取代`。
  **不默认用连续数字编号**——那要多一个分配中心，多 worktree 并行时还会抢号。
- **什么时候写、唯一来源、取代流程、升级到重型机制的条件：见上面链的 ADR 备忘录**——本页不复述。
  （刻意如此：细则双源 = 重演副本烂账，那正是这套流程当年踩过的坑。）
