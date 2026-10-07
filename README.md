# ozon-dropship

**一套自用的履约中台**：Ozon 店铺来单后自动接住，替你去中国货源平台下单代发，盯着中转点收货，再把国际快递单号回传回 Ozon——把「一天几百上千单全靠人手工搬」这件事交给程序。

## 前因后果

- 我们在 Ozon（俄罗斯电商平台）上开着 10 多家店，日出单量 200～1000 单。
- 现成 ERP 在**采购这一段全是半自动**：还得人一张张去 1688 下单、对着表格回填单号——最费人的那一段恰恰没被自动化。
- 评估下来，现成方案改不动，也接不上我们要用的 1688 自用下单通道。
- 所以 owner 拍板**直接自研**（[ADR-20261007-build-in-house](docs/decisions/2026-10-07-build-in-house.md)）。
- 完整的前因后果、Ozon / 中国货源 / 物流三侧的调研结论与来源，都在 [Issue #1](https://github.com/Strelizialeomon/ozon-dropship/issues/1)。

## 现在到哪了

S1（地基 + 单店端到端闭环）的**六份代码已全部写完并合并**，六份都挂着「待上线」。

**当前唯一的阻塞项**：S1-C（1688 客户端）挂着「受阻」——在等 1688 的「买家自用版」权限批下来（[issue #8](https://github.com/Strelizialeomon/ozon-dropship/issues/8)，下次看 2026-10-14）。另有一张 S1-D 的白屏修复单（[issue #20](https://github.com/Strelizialeomon/ozon-dropship/issues/20)）也未关。

**上线还缺的前置条件（5 项，照[总纲 §12.1](docs/specs/spec-fulfillment-hub.md)）**：1688 开放平台「买家自用版」权限（正在申请——就是它卡着 S1-C）、Ozon 各店凭据（`Client-Id` / `Api-Key`）、中转点（用哪家货代仓或自有仓）、服务器 + 固定公网 IP + 域名 + 线路实测、合规咨询（俄罗斯第 152 号联邦法）。

## 怎么用起来

**还没上线**——代码写完了，要真跑起来还差上面那 5 项前置。

要上线时照交接手册的[〈从零上线〉](docs/handover.md)走：**要买什么、要准备哪些账号与凭据、每步大概多久、卡住看哪**都写在那份里；每个命令的细则以 [deploy/install.md](deploy/install.md) 为唯一真相源。

## 文档地图

| 你是谁 | 从哪看起 |
|---|---|
| **接手的你（不写代码）** | **[docs/handover.md](docs/handover.md)** —— 交接手册：系统是什么、现在到哪、怎么上线、日常怎么用、下一步的活怎么交代出去 |
| 要看设计 / 改代码 | [总纲](docs/specs/spec-fulfillment-hub.md)（设计权威）→ [决策记录（ADR）](docs/decisions/) → [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md) |
| 要部署 / 运维 | [deploy/install.md](deploy/install.md)（首次安装）、[deploy/release.md](deploy/release.md)（日常发布与回滚）、[deploy/backup/restore-drill.md](deploy/backup/restore-drill.md)（恢复演练） |

目录速查：`backend/`（Go 后端）、`frontend/`（React 操作台）、`deploy/`（部署与备份脚本）、`docs/`（spec 与决策记录）。前后端依赖各自分开，见 [ADR-20261007-repo-layout](docs/decisions/2026-10-07-repo-layout.md)。

## 这个仓怎么干活

- **需求 → 设计 → 实施**走 spec-flow 两段流程：先出 spec 放 `docs/specs/`，再开 issue 实施，每份交付物合并前都要 owner 点一道审核档位。
- **长期决定**写进[决策记录（ADR）](docs/decisions/)：一条决定一个文件，定了就不再反复重开。
- **每个 issue 挂状态标签**（未开始 / 进行中 / 待上线 / 待验证），卡住的另挂「受阻」「等拍板」——想知道某件事卡在哪，看标签就行。

细则都在 [AGENTS.md](AGENTS.md)，本节不复述。
