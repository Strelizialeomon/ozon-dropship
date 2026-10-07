# spec-open-source —— 开源改造（独立 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」）。本文是**独立需求**，不属 S1 六份子 spec；与总纲或生效 ADR 冲突时以它们为准。
> Issue: [#27](https://github.com/Strelizialeomon/ozon-dropship/issues/27)（本份的实施单）｜ 状态：v1.2（2026-10-07：PR #28 轻审处置——修订 M1 与 L2/L4、回填实施单号，见 §7）
> 发布序：随时可开；**「翻公开」是最后一步——实施 PR 合并后、owner 单独点头才执行**。
> 跨份验收与协作声明：无（独立需求）；与交接线（已落地：PR #24）共用 `README.md`，衔接规则见 §1。
> 长期决定见 [ADR-20261007-open-source](../decisions/2026-10-07-open-source.md)（本文只链接、不复述决定正文；ADR 与本文不一致时以 ADR 为准）。

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| 新增 `LICENSE`（AGPL-3.0 官方全文） | 代码与功能的任何改动（`backend/`、`frontend/`） |
| 根 `README.md` 的**公开化增量**（以交接线已落地版为基础，见 §3.2） | 交接线（已落地：PR #24）的产物：`docs/handover.md`、spec 状态行回填——一律不碰；`docs/decisions/README.md` 一览表只补本 ADR 一行（机械回填，见 §6） |
| 新增 `CONTRIBUTING.md`、`SECURITY.md`、根 `.gitignore` | 历史重写（提交邮箱保留，owner 已接受） |
| 新增 `docs/decisions/2026-10-07-open-source.md`（本份自带 ADR，随本 PR 落） | 其余 spec / ADR 正文（一个字不动） |
| GitHub 仓库设置（description / topics / 私密漏洞报告）与最后一步「翻公开」 | CI（机制卡未选；将来另议） |

**与交接线的衔接**：交接线（issue #23）已落地（PR #24 已合并）：根 README 已重写为非技术门口层（一句话 / 前因后果 / 现在到哪了 / 怎么用起来 / 文档地图 / 这个仓怎么干活）。本份**不重写、只增量**：在其版本上加公开向的节（§3.2）；实施时先 `git fetch` 对表，以彼时 main 上的 README 为准。

## 2. 背景与现状

2026-10-07 owner 拍板：项目整体开源——许可证 **AGPL-3.0**；文档 / issue / PR 随仓库**全公开**；机制卡点选三件套（LICENSE + ADR、README + 协作文件、根 `.gitignore`），**不加 CI**；与交接线排序「交接先行」——交接线已落地（PR #24 已合并、issue #23 已关），本份实施按 §1 衔接。

**现状问题**（2026-10-07 实测，非推测）：

| # | 问题 | 证据 |
|---|---|---|
| 1 | 仓库私有且无许可证 | `gh repo view`：`visibility=PRIVATE`、`licenseInfo=null`；没有 LICENSE 的「公开」= 默认版权，法律上谁都不能用 |
| 2 | README 已重写、但还不是给外人的 | 交接线已把根 README 重写为**非技术门口层**（PR #24，43 行，读者＝接手人）；公开向仍缺：英文摘要 / 许可证徽章 / 开发者快速开始 / 参与 / 许可——本份按 §3.2 增量补 |
| 3 | 无协作与安全惯例件 | 无 `CONTRIBUTING.md`、`SECURITY.md`；无根 `.gitignore`（`backend/`、`frontend/` 各有一份）；无 `.github/`、无 `scripts/` |
| 4 | 本地垃圾未挡 | `.claude/`（本地 worktree 目录）处于 untracked 游离态，随时可能被误提交 |

**公开前的实测扫描**（2026-10-07，全库 + 全历史，只读）：

| 项 | 结果 |
|---|---|
| 密钥 / 凭据 | 无——配置全是占位符（`config.example.yaml`、`hub.example.com`、`1.2.3.4`），`testdata` 与 `_test` 内为假值 |
| 真实域名 / IP / 邮箱 | 无真实环境信息——占位符（`hub.example.com`、`1.2.3.4` 等）与公开资料（Ozon 官方 IP 段）；**例外**：`backend/internal/alibaba/testdata/` 含 1688 **官方样例**数据（示例邮箱 / 手机号 / 姓名；`testdata/README.md` 已注明「官方样例原样保留」），属公开资料、非 owner 个人信息 |
| 个人信息 | 提交历史作者含个人邮箱与 GitHub noreply 两个身份——owner 知悉，**保留不重写** |
| 大文件 / 二进制历史 | 无（size-pack ≈ 673 KiB；无 submodule、无 LFS） |
| 外部引用 | `AGENTS.md` 指向的 spec-flow 真身是**公开仓**，公开后链接可达 |

**公开后的暴露面**（owner 已知悉并接受）：`docs/specs/` 全部设计文档、`docs/decisions/` 全部 ADR、全部 issue 与 PR（含调研与流程记录）。

## 3. 要做的事

### 3.1 新增 `LICENSE`

AGPL-3.0 官方全文（gnu.org 原件，逐字不改、不删「How to Apply」节）。

### 3.2 根 `README.md` 公开化增量

在交接线版本基础上，**增**下列内容（不动其既有节）：

1. **顶部英文摘要**（2–3 行，给非中文读者一个落点；正文仍中文）。
2. **许可证徽章**（AGPL-3.0，链 `LICENSE`）+ 梗概里一句话点明开源。
3. **开发者快速开始**：后端 / 前端各几条命令（含前提：Go / bun / MySQL / Redis）；细节链 `CONTRIBUTING.md`，**不复述**（沿交接线「技术细节不进正文」原则）。
4. **参与**：一段话 + 链 `CONTRIBUTING.md`、`SECURITY.md`。
5. **许可**：AGPL-3.0、`Copyright (C) 2026 <署名>`、一句话含义（用了改了要同样开源，网络服务也算）。

### 3.3 新增 `CONTRIBUTING.md`

一页以内：环境与构建（后端 `go build ./...` / `go test ./...`，前端 `bun install` / `bun test` / `bun run build`，以实测为准）、代码风格（`golangci-lint`、`dprint`）、提交与 PR 习惯（中文提交信息、conventional 前缀、小步 PR）、贡献授权声明（提交即同意以 AGPL-3.0 授权）。

### 3.4 新增 `SECURITY.md`

一页以内：**不要开公开 issue 报漏洞**；走 GitHub 私密漏洞报告（Security → Report a vulnerability，翻公开后同步开启）；重点范围（凭据保险箱、API 凭据处理、登录会话）；维护者尽力而为、不作时限承诺。

### 3.5 新增根 `.gitignore`

只放本地 / OS / 编辑器垃圾：`.DS_Store`、`.claude/`、`.idea/`、`.vscode/`、`*.swp`。子目录既有 `.gitignore` 一律不动。

### 3.6 仓库元数据

- description：现文案基础上补「开源（AGPL-3.0）」一句。
- topics：`ozon`、`1688`、`dropshipping`、`fulfillment`、`golang`、`react`、`cross-border-ecommerce`。
- 开启 GitHub 私密漏洞报告（翻公开后执行，与 §3.4 呼应）。

### 3.7 「翻公开」（最后一步）

1. 公开前**重扫一遍**（全库 + 全历史），结果贴实施 issue；
2. owner 点头；
3. `gh repo edit --visibility public`（含 GitHub 的一次性确认）；
4. 匿名（无登录）复验：README、`LICENSE`、issue #1、`docs/specs/` 可达。

### 3.8 实施节奏

本份的实施（§3.1–§3.6）另开 issue、从段二进；§3.7 单独留在实施 issue 收尾，**不随实施 PR 一起做**。

## 4. 机制清单

| # | 机制 | 解决什么 / 没有它会怎样 |
|---|---|---|
| 1 | `LICENSE`（AGPL-3.0） | 解决「公开了但法律上谁也不能用」；没有它：默认版权，他人无授权 |
| 2 | `ADR-20261007-open-source` | 解决「开源这裁决没人知道，下个 agent / 合作者按私有仓处理」；没有它：同类决策被重拍、依赖许可会踩 AGPL 的雷 |
| 3 | README 公开化增量 | 解决「陌生人第一眼看到的是内部交接文档」；没有它：来的人找不到定位与入口 |
| 4 | `CONTRIBUTING.md` | 解决「想参与的人不知道从哪下手」；没有它：贡献质量参差、维护成本全落到 owner |
| 5 | `SECURITY.md` + 私密漏洞报告 | 解决「漏洞没处私密报」；没有它：漏洞只能扔公开 issue，公开即引爆 |
| 6 | 根 `.gitignore` | 解决「本地垃圾（`.claude/`、`.DS_Store`）误入公开仓」；没有它：首个外部 PR 前先被垃圾文件糊脸 |
| 7 | 「翻公开」+ 公开前重扫 | 解决「件齐了但仓库还是私有」与「公开时把漏网敏感物一起抖出去」；没有它：开源没生效，或公开后才发现问题 |
| 8 | 仓库元数据（description / topics） | 解决「公开后仓库在搜索、列表里毫无说明」；没有它：陌生人搜到也看不出这是什么 |
| 9 | 与交接线衔接规则（§1） | 解决「两条线先后改同一份 `README.md`、互相覆盖」；没有它：实施时把交接线的门口层改坏或重复劳动 |

## 5. 验收

- [ ] `LICENSE` 与 gnu.org 原作逐字一致（下载原件比对，零 diff）
- [ ] 陌生开发者只读 README + CONTRIBUTING：后端能构建、前端能装依赖并构建（`go build ./...`、`bun install && bun run build` 实测通过；需外部服务的步骤已注明前提）
- [ ] README 含英文摘要 / 徽章 / 开发者快速开始 / 参与 / 许可五件，且交接线既有节未被破坏
- [ ] `CONTRIBUTING.md` 与 `SECURITY.md` 存在；README 指向它们的链接可达
- [ ] 根 `.gitignore` 生效：`git status` 不再出现 `.claude/`、`.DS_Store`
- [ ] ADR 已落 `docs/decisions/2026-10-07-open-source.md`（**随本 PR**，不是实施 issue）；`docs/decisions/README.md` 一览表已收编本行
- [ ] 公开前重扫：全库 + 全历史无密钥，结果贴实施 issue
- [ ] description / topics 已更新；私密漏洞报告已开启
- [ ] **owner 点头后**翻公开；匿名可达 README、`LICENSE`、issue #1、`docs/specs/`
- [ ] 文档链接全绿：相对链接 0 失效；`AGENTS.md` 指向的 spec-flow 真身可达

## 6. 自定细节

**体例与落点**

- README 中文为主 + 顶部英文摘要；不加完整英文版（要加另说）。
- 快速开始细节放 `CONTRIBUTING.md`，README 只留最短命令 + 链接。
- 版权署名写 `Strelizialeomon`（GitHub 主身份）；要换法定姓名 / 公司名，实施前说一声。
- **不加**：源文件 license header、`CODE_OF_CONDUCT.md`、CI（机制卡未选）。
- 公开前重扫结果、翻公开动作，都留痕在实施 issue 的评论里。
- 本 ADR 随本 PR 收编进 `docs/decisions/README.md` 一览表（机械回填，属豁免）；实施时复核该行仍在、未被后续合并冲掉。

**口径与判据（未写死的数字都是 agent 定的，owner 扫一眼即可）**

- `LICENSE` 一律取 gnu.org / SPDX 官方原件，不手打、不复述。
- 「跑得起来」门槛 = §5 第 2 条那两条构建命令实测通过；MySQL / Redis 只注明前提，不要求陌生人当场装。
- 依赖许可兼容性：实施时抽查 `go.mod` 与 `package.json` 直接依赖（MIT / Apache-2.0 / BSD 系均兼容 AGPL）；发现不兼容项即停下报告。
- 提交邮箱不重写；`.mailmap` 也不加（owner 已接受现状）。
- topics 用 §3.6 词表，实施时可微调。
- 翻公开用 `gh repo edit --visibility public`；若 gh 版本要求交互确认，改为输出命令由 owner 执行。
- CONTRIBUTING 含「提交即同意以 AGPL-3.0 授权」声明；CONTRIBUTING 与 `SECURITY.md` 篇幅上限「一页以内」；SECURITY 响应口径写「尽力而为、不作时限承诺」。
- README 顶部英文摘要 2–3 行封顶。

## 7. 审核修订记录

**第 1 次**（PR #25 闸 · 轻审 · 2026-10-07）：1 个只读审核 agent 出 5 条发现（1 中 / 4 低），owner 点「全改」。逐条处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 1 | 中 | 交接线状态断言已过期（写时属实；审核期间 PR #24 已合、#23 已关，README 已重写为 43 行） | 改：头部 / §1 / §2 改「已落地（PR #24）」；衔接规则改纯增量；分支追平 main |
| 2 | 低 | §6 括注「若翻公开先于交接线」的假设分支已失效、且与拍板排序互斥 | 改：删该分支，改为「随本 PR 收编一览表」 |
| 3 | 低 | 扫描行「邮箱 / 个人信息 = 无」口径过绝对（testdata 含 1688 官方样例数据） | 改：§2 扫描表与 ADR「依据」行内点名例外 |
| 4 | 低 | §4 机制清单漏 2 候选（仓库元数据、衔接规则） | 改：清单补为 9 行 |
| 5 | 低 | §6 自定细节未回列 4 条 agent 自定口径 | 改：回列（授权声明 / 一页以内 / 尽力而为 / 英文摘要 2–3 行） |

审核结论原文（未删减）见 [PR #25 评论](https://github.com/Strelizialeomon/ozon-dropship/pull/25#issuecomment-6038207822)。

**第 2 次**（PR #28 闸 · 轻审 · 2026-10-07）：1 个只读审核 agent 出 6 条发现（1 中 / 5 低），owner 点「全改」（低 1/3 属计划内 / 既定取舍，不改）。逐条处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| M1 | 中 | CONTRIBUTING 建表路径缺 `CREATE DATABASE`、DSN 未提示同步改——干净机器断在第一步 | 改：补建库一步 + DSN 提示；注明无 AutoMigrate、两步都要有 |
| L2 | 低 | 「测试不需要外部服务」属实，但未提 build tag 集成测试 | 改：补一行 + 链 `backend/ARCHITECTURE.md` |
| L4 | 低 | 环境表漏 goose / golangci-lint；给的是生产向安装法 | 改：环境表补工具行（含本机安装法）；§7 注「生产环境」 |
| L5 | 低 | spec 头部实施单号未回填 | 改：回填 #27（本条即 L5 的落地） |
| L1 | 低 | README 宣称已开源、仓库仍 PRIVATE | 不改：计划内过渡态（§3.7 翻公开为最后一步） |
| L3 | 低 | `.claude/` 被根 `.gitignore` 挡（共享配置需 `-f`） | 不改：既定取舍（§3.5 点名要这条） |

审核结论原文（未删减）见 [PR #28 评论](https://github.com/Strelizialeomon/ozon-dropship/pull/28#issuecomment-6039167665)。
