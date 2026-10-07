# deploy —— S1-F 部署

单机部署的落地文件（设计：[总纲 §4.1](../docs/specs/spec-fulfillment-hub.md) 与
[ADR-20261007-deployment](../docs/decisions/2026-10-07-deployment.md)）：
不用 Docker；Caddy 管 HTTPS 与前端静态资源；systemd 托管 Go；MySQL 8.4 / Redis 装本机。

## 文件地图（→ 服务器落点）

| 本目录 | 落点 | 干什么 |
|---|---|---|
| `Caddyfile` | `/etc/caddy/Caddyfile` | HTTPS、静态资源 + SPA 回退、`/api` `/hooks` 反代 |
| `systemd/fulfillment-hub.service` | `/etc/systemd/system/` | 托管 Go（主密钥经加密凭据注入） |
| `systemd/fulfillment-hub-backup.{service,timer}` | `/etc/systemd/system/` | 每日备份定时器 |
| `mysql/fulfillment-hub.cnf` | `/etc/mysql/mysql.conf.d/` | binlog 保留 30 天 |
| `redis/fulfillment-hub.conf` | `/etc/redis/`（include 进 redis.conf） | AOF 开 |
| `backup/backup.sh` | `/usr/local/bin/hub-backup` | 每日 dump + binlog 外送 + 两端清理 |
| `backup/backup.{cnf,env}.example` | `/etc/fulfillment-hub/backup.{cnf,env}` | 备份凭据与目的地（0600） |
| `line-test/` | 三台按量机临时跑 | 线路实测（报告贴 S1 父 issue） |

**从零安装照 [install.md](install.md)；日常发布照 [release.md](release.md)。**

## 服务器目录布局

```
/opt/fulfillment-hub/      后端：api 二进制、config/config.yaml、migrations/
/srv/fulfillment-hub/web/  前端构建产物（frontend/dist 同步过来）
/etc/fulfillment-hub/      备份凭据、迁移 DSN（均 0600）
/etc/credstore.encrypted/  systemd 加密主密钥
```

## 与 S1-A / S1-E 的对接点（本目录先钉住，合并时对表）

| 约定 | 值 | 落地在哪 |
|---|---|---|
| Go 监听 | `127.0.0.1:8080`（只回环，TLS 归 Caddy） | 后端 config；Caddyfile 反代到这个地址 |
| 配置文件 | `/opt/fulfillment-hub/config/config.yaml`（工作目录 = `/opt/fulfillment-hub`） | systemd unit 的 `WorkingDirectory` |
| 二进制 | `/opt/fulfillment-hub/api`（由 `./cmd/api` 构建） | unit 的 `ExecStart`；release.md 的构建命令 |
| 主密钥凭据 | 名字 `hub-vault-master-key`，内容 32 字节随机值 | install.md §6 生成；unit `LoadCredentialEncrypted`；S1-A 保险箱读 `$CREDENTIALS_DIRECTORY` |
| 静态资源目录 | `/srv/fulfillment-hub/web`（= `frontend/dist` 的内容） | Caddyfile 的 `root` |

> unit 还带基础进程加固（NoNewPrivileges / PrivateTmp / ProtectSystem=strict / ProtectHome）：
> `ProtectSystem=strict` 下应用只能写 `/opt/fulfillment-hub`——S1-A 实装若有别的落盘路径，对表时改 `ReadWritePaths`。
> F 的合并序在 A、E 之后——那时代码已成真，以上五条逐一对表；有出入以 A / E 实装为准，改本目录。

## 验收对照（issue #11）

| 验收 | 怎么验 |
|---|---|
| 从零装好：HTTPS、任意路由刷新不 404、`/api` 通 | [install.md](install.md) §12 清单 |
| 重启自动起；主密钥不入环境变量与进程参数 | [install.md](install.md) §8、§12 |
| Redis AOF 已开 | `redis-cli CONFIG GET appendonly appendfsync` |
| 备份跑一次；恢复到指定时刻 | [backup/restore-drill.md](backup/restore-drill.md) |
| 线路实测报告贴父 issue | [line-test/README.md](line-test/README.md)（需先租三地按量机，owner 点头后跑） |
