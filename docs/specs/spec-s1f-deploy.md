# spec-s1f-deploy —— S1-F 部署（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-F**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: 待开 ｜ 状态：v1.0（2026-10-07）｜ 发布序：随时可开；**须在 A、E 之后合并**（验收要 S1-A 的二进制与 S1-E 的构建产物）
> 跨份验收与协作声明：S1 父 issue（待开）

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| 仓库根 `deploy/`：Caddy、systemd、MySQL / Redis 配置、备份与恢复、发布说明 | 代码（S1-A ~ S1-E） |
| 服务器线路实测与选址报告 | 在真实生产机上做任何改动（每次都要 owner 同意） |

## 2. 地盘

- **独占**：仓库根 `deploy/**`。
- **不碰**：`backend/**`、`frontend/**`。

## 3. 要做的事

照 [部署 ADR](../decisions/2026-10-07-deployment.md)：

1. **Caddyfile**：自动 HTTPS；`/` 托管前端静态资源（S1-E 构建产物 `frontend/dist`），找不到文件回退 `index.html`；`/api/*`、`/hooks/*` 反代到 Go。
2. **systemd unit**：托管 Go 程序；主密钥用 `LoadCredentialEncrypted` 注入，不放环境变量。
3. **MySQL 8.4 与 Redis**：安装与配置说明；Redis 开 `appendonly yes` + `appendfsync everysec`。
4. **备份**：每天 `mysqldump --single-transaction` 传到另一台机器或对象存储；binlog 保留 30 天；写恢复演练步骤（总纲 §13.1）。
5. **发布说明**：后端二进制与前端静态资源分开发布。
6. **线路实测**：在国内、香港、新加坡各开一台按量机，测到 `api-seller.ozon.ru` 与 1688 开放平台的延迟和失败率，报告贴 S1 父 issue，由 owner 拍板服务器放哪（总纲 §13.2）。

## 4. 验收

- [ ] 在一台测试机上照 `deploy/` 从零装好：HTTPS 可访问、前端任意路由刷新不 404、`/api` 通到 Go
- [ ] systemd 重启后 Go 自动起来；主密钥不出现在环境变量与进程参数里
- [ ] Redis AOF 已开
- [ ] 备份脚本跑一次；恢复演练能恢复到指定时刻
- [ ] 线路实测报告已贴父 issue

## 5. 机制清单

无新增机制。落地总纲 §4.1 部署拓扑与 §5.5 中「Redis 开 AOF」一项。

## 6. 自定细节

- `deploy/` 放仓库根：它同时服务前后端，不属于任何一个子项目。
- 测试机与线路实测机都用按量云主机，用完释放。
- 线路实测只调只读接口，每地持续 24 小时，看 P50 / P95 延迟与失败率。

## 7. 审核修订记录

PR #4 重审（owner 2026-10-07 点选「改」）的处置：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 4-9 | 轻微 | 合并时点悬空 | 改：定「须在 A、E 之后合并」；总纲 §12.3 同步 |
| 4-15 | 轻微 | 静态资源目录未写死 | 改：定 `frontend/dist` |

PR #12 重审（owner 2026-10-07 点选「改」）的处置，原文见 PR #12 评论（路一 = 规格符合性，路二 = 对抗式找 bug）：

| # | 严重度 | 发现 | 处置 |
|---|---|---|---|
| 12-1 | 严重 | 最新 dump 的坐标 binlog 文件被排除在外送之外：最新备份不可回放，最坏丢 24h 且演练会掩盖 | 改：dump 后补一次 `FLUSH LOGS` 关闭坐标文件再外送；restore-drill §5 写明真实 RPO 与收紧杠杆 |
| 12-2 | 严重 | 备份 oneshot 无超时 + flock 独占：ssh/rsync 挂死后备份永久静默停摆 | 改：unit 加 `TimeoutStartSec=30min`；ssh 加 ConnectTimeout/ServerAlive/BatchMode，rsync 加 `--timeout=600` |
| 12-3 | 中 | 演练命令无实例参数，照抄会打生产库 | 改：命令统一经 `$DRILL_MYSQL` 指向演练实例，并加「先自查实例」防呆步 |
| 12-4 | 中 | 备份 unit 缺 `After=mysql.service`，`Persistent=true` 开机补跑会撞未就绪的库 | 改：加 `After=`/`Wants=mysql.service` |
| 12-5 | 中 | install.md 的 scp/sudo 隐式假设矛盾（root 目标目录 vs 处处 sudo） | 改：§0 声明「root 经 ssh 操作」模型与普通用户 + sudo 的等效做法 |
| 12-6 | 中 | goose 无安装命令；migrate.env 非 root 读不到 | 改：补交叉编译安装命令；迁移执行包进 `sudo sh -c` |
| 12-7 | 中 | config.yaml 含 DB 密码却默认 0644 | 改：scp 落 /tmp 编辑后 `install -o root -g hub -m 640` 落位 |
| 12-8 | 轻微 | redis include 注释对目标发行版不成立 | 改：删该括号说明，明写「包默认无可用 include 目录」 |
| 12-9 | 轻微 | systemd 进程加固未披露 | 改：PR 正文补披露；deploy/README 加对表提示（ReadWritePaths） |
| 12-10 | 轻微 | 线路实测 auth.env（临时机上放只读凭据）未披露 | 改：PR 正文补披露 |
| 12-11 | 轻微 | 回放需 `BINLOG_ADMIN` 未写明，用备份账号会半途中止 | 改：restore-drill §3 写明回放用 root |
| 12-12 | 轻微 | nohup 后台运行时 SIGINT 被忽略，停不掉 | 改：README 与脚本注释明确「用 TERM（kill）停」 |
| 12-13 | 轻微 | raw.csv 标签含逗号会错列、统计全错 | 改：标签写入前替换逗号 |
| 12-14 | 轻微 | §8 取证 `tr < /proc/$pid/environ` 重定向发生在 sudo 之前，非 root 读不到 | 改：包进 `sudo sh -c` |
| 12-15 | 轻微 | 未装 rsync/rclone，备份外送可能 `command not found` | 改：安装节补 `apt install -y rsync`（rclone 方案同理） |
