# install —— 从零安装手册（Ubuntu 24.04 LTS / Debian 12+）

> 照本文件在一台**干净测试机**上从头装完，即完成验收清单（§12）前三、四条的技术部分。
> 规则（ADR）：不用 Docker；Caddy 管 HTTPS；systemd 托管 Go；MySQL 8.4 与 Redis 装本机。
> 下文 `<server>` 换成服务器地址（建议在 `~/.ssh/config` 里起个别名）；命令里的
> 「构建机」= 你的开发机。测试机与线路实测机都用按量云主机，用完释放。

## 0. 前提

- Ubuntu 24.04 LTS（推荐）或 Debian 12+：**systemd ≥ 250**（加密凭据要用；`systemctl --version` 查）。
- 固定公网 IP + 一个域名，A 记录已指向本机。
- 防火墙 / 云安全组放行 **80、443**（Caddy 证书）与 22。
- **操作方式**：本手册按「以 root 经 ssh 操作服务器」写——`<server>` 指可 root 登录的地址（例
  `root@1.2.3.4`，或 `~/.ssh/config` 里 root 身份的别名）；先在开发机 `ssh-copy-id root@<ip>`
  （云镜像默认只许密钥登录 root，正好）。若你坚持普通用户 + sudo：把每条 `scp` 改成先落 `/tmp`、
  再用 `sudo install/mv` 落位，语义不变。
- 装完大约 1 小时。

## 1. 用户与目录

```bash
sudo useradd --system --user-group --home-dir /opt/fulfillment-hub --shell /usr/sbin/nologin hub
sudo install -d -m 0755 /opt/fulfillment-hub/config /opt/fulfillment-hub/migrations
sudo install -d -m 0755 /srv/fulfillment-hub/web
sudo install -d -m 0750 /etc/fulfillment-hub
```

## 2. 装三个组件

```bash
# MySQL 8.4：到 https://dev.mysql.com/downloads/repo/apt/ 拿当前 mysql-apt-config 的 .deb 链接
wget -O /tmp/mysql-apt-config.deb '<上一步拿到的链接>'
sudo dpkg -i /tmp/mysql-apt-config.deb        # 对话框里选 MySQL 8.4 LTS
sudo apt update && sudo apt install -y mysql-server

# Redis（发行版自带的 7.x 即可用）
sudo apt install -y redis-server

# 备份外送要用（rsync 方案；对象存储方案就把这行换成 rclone）
sudo apt install -y rsync

# Caddy（官方 apt 源，照官网）
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

## 3. MySQL：binlog 保留 30 天

把 `deploy/mysql/fulfillment-hub.cnf` 复制到 `/etc/mysql/mysql.conf.d/`，然后：

```bash
sudo systemctl restart mysql
sudo mysql -e "SHOW VARIABLES WHERE Variable_name IN ('log_bin','binlog_expire_logs_seconds','server_id');"
# 期望：log_bin=ON、binlog_expire_logs_seconds=2592000、server_id=1
```

## 4. MySQL：建库、应用账号、备份账号

`sudo mysql` 进交互（root 若有密码就 `mysql -u root -p`），执行：

```sql
CREATE DATABASE fulfillment_hub CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- 应用账号（后端 config 用；两个密码都各自随机生成，别复用）
CREATE USER 'hub'@'localhost' IDENTIFIED BY '替换-应用密码';
GRANT ALL PRIVILEGES ON fulfillment_hub.* TO 'hub'@'localhost';

-- 备份账号（权限照 mysqldump 官方口径：--source-data 要 REPLICATION CLIENT + RELOAD）
CREATE USER 'hub_backup'@'localhost' IDENTIFIED BY '替换-备份密码';
GRANT SELECT, SHOW VIEW, TRIGGER, PROCESS, RELOAD, REPLICATION CLIENT
  ON *.* TO 'hub_backup'@'localhost';
```

## 5. Redis：开 AOF

把 `deploy/redis/fulfillment-hub.conf` 复制到 `/etc/redis/fulfillment-hub.conf`，
在 `/etc/redis/redis.conf` 末尾加一行 `include /etc/redis/fulfillment-hub.conf`，然后：

```bash
sudo systemctl restart redis-server
redis-cli CONFIG GET appendonly appendfsync   # 期望：yes / everysec
```

## 6. 主密钥与加密凭据

主密钥加密数据库里的店铺凭据（总纲 §5.4）。后端实装（`backend/internal/infra/vault`）要的是一份
**Tink keyset JSON**，用仓库自带的 `backend/cmd/genvaultkey` 生成。**生成后必须离机留一份**——
加密凭据绑定本机，机器换/重装后，旧机器上的文件解不开，只有你手里的那份能重建
（见 `backup/restore-drill.md` §4）。

在**构建机**上生成（仓库 `backend/`）：

```bash
go run ./cmd/genvaultkey -out /tmp/vault_keyset.json
# ↑ 生成 Tink AES256-GCM keyset JSON（0600）。把整份 JSON 文本抄进密码管理器 = 离机备份。
```

传到服务器、加密入库（这份 JSON 就是主密钥明文：别走 git / 聊天工具 / 邮件）：

```bash
sudo install -d -m 0700 /etc/credstore.encrypted
scp /tmp/vault_keyset.json <server>:/root/vault_keyset.json
ssh <server>
sudo systemd-creds encrypt --name=vault_keyset.json \
  /root/vault_keyset.json /etc/credstore.encrypted/vault_keyset.json
sudo chmod 600 /etc/credstore.encrypted/vault_keyset.json
sudo shred -u /root/vault_keyset.json
```

构建机上的明文也清掉：`shred -u /tmp/vault_keyset.json 2>/dev/null || rm -f /tmp/vault_keyset.json`

> 文件名 `vault_keyset.json` 三处必须一致，否则服务起不来（`vault.New` 失败即 `logger.Fatalf`）：
> `systemd-creds encrypt --name=`、单元的 `LoadCredentialEncrypted=`、后端配置
> `vault.master_key_file`（默认即此名）。

## 7. 后端首次部署（二进制 + 配置 + 迁移）

在**构建机**上（仓库 `backend/`）：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/hub-api ./cmd/api
# 服务器是 ARM 架构就改 GOARCH=arm64
scp /tmp/hub-api <server>:/opt/fulfillment-hub/api
scp -r backend/migrations <server>:/opt/fulfillment-hub/
# goose CLI（迁移用；交叉编译后传上去。也可从 goose 官方 releases 下 linux 二进制，版本自定）
GOOS=linux GOARCH=amd64 GOBIN=/tmp go install github.com/pressly/goose/v3/cmd/goose@latest
scp /tmp/goose <server>:/usr/local/bin/goose
```

配置（字段以 `backend/config/config.example.yaml` 为准；要点：监听 `127.0.0.1:8080`、
MySQL 用 `hub`@localhost、Redis 本机）。**必改一处**：`vault.credentials_dir` 改成
`/run/credentials/fulfillment-hub.service`（凭据目录名 = systemd 单元名；example 里的
`ozon-dropship.service` 是旧占位，不改则服务起不来）：

```bash
scp backend/config/config.example.yaml <server>:/tmp/config.yaml
ssh <server>  # 编辑 /tmp/config.yaml 填生产值，然后落位并收紧权限（里面有 DB 密码）：
#   install -o root -g hub -m 640 /tmp/config.yaml /opt/fulfillment-hub/config/config.yaml
```

迁移（goose CLI，装到服务器 `/usr/local/bin/goose`；DSN 参数与配置保持一致）：

```bash
ssh <server>
sudo sh -c 'cat > /etc/fulfillment-hub/migrate.env <<EOF
GOOSE_DRIVER=mysql
GOOSE_DBSTRING=hub:替换-应用密码@tcp(127.0.0.1:3306)/fulfillment_hub?parseTime=true&multiStatements=true
EOF
chmod 600 /etc/fulfillment-hub/migrate.env'
sudo sh -c 'set -a; . /etc/fulfillment-hub/migrate.env; set +a; \
  goose -dir /opt/fulfillment-hub/migrations up'
```

## 8. systemd：装单元、开机自启、核验

```bash
scp deploy/systemd/fulfillment-hub.service <server>:/etc/systemd/system/
ssh <server> 'systemctl daemon-reload && systemctl enable --now fulfillment-hub'
ssh <server> 'systemctl status fulfillment-hub --no-pager'
```

主密钥不进环境变量、不进进程参数（验收第 2 条）——在服务器上：

```bash
pid=$(systemctl show -p MainPID --value fulfillment-hub)
sudo sh -c "tr '\0' '\n' < /proc/$pid/environ" | grep -iE 'key|secret|master' || echo '环境变量里没有密钥 ✓'
sudo ps -o args= -p $pid                       # 进程参数里也没有 ✓
sudo ls /proc/$pid/root/run/credentials/fulfillment-hub.service/
# ↑ 能看到 vault_keyset.json = 进程读到了凭据
```

重启自动起（验收第 2 条）：`sudo reboot` 后回来 `systemctl is-active fulfillment-hub` 应为 `active`
（`systemctl enable` 已做；reboot 才是真验收）。

## 9. Caddy：HTTPS 与反代

把 `deploy/Caddyfile` 里的域名换成你的（`hub.example.com` → 实际域名），拷上去：

```bash
scp deploy/Caddyfile <server>:/etc/caddy/Caddyfile
ssh <server> 'systemctl reload caddy'
curl -sS -o /dev/null -w '%{http_code}\n' https://<你的域名>/
```

首次访问 Caddy 自动申证书（要 80/443 可达 + DNS 正确）；`journalctl -u caddy -f` 可看申请过程。

## 10. 前端静态资源

在**构建机**（仓库 `frontend/`）：

```bash
bun install --frozen-lockfile && bun run build
rsync -a --delete frontend/dist/ <server>:/srv/fulfillment-hub/web/
```

## 11. 备份

```bash
# 备份机/对象存储先就绪：rsync 方案配 root 免密 ssh；rclone 方案先 rclone config
scp deploy/backup/backup.sh <server>:/usr/local/bin/hub-backup
scp deploy/backup/backup.cnf.example deploy/backup/backup.env.example <server>:/tmp/
scp deploy/systemd/fulfillment-hub-backup.service deploy/systemd/fulfillment-hub-backup.timer \
  <server>:/etc/systemd/system/

ssh <server>
sudo chmod 755 /usr/local/bin/hub-backup
sudo install -m 0600 /tmp/backup.cnf.example /etc/fulfillment-hub/backup.cnf   # 改密码
sudo install -m 0600 /tmp/backup.env.example /etc/fulfillment-hub/backup.env   # 填配置
rm /tmp/backup.cnf.example /tmp/backup.env.example
sudo systemctl daemon-reload
sudo systemctl enable --now fulfillment-hub-backup.timer

# 首跑一次（验收第 4 条前半）：
sudo systemctl start fulfillment-hub-backup
sudo journalctl -u fulfillment-hub-backup -n 50 --no-pager
# 再去备份机看一眼：db/ 下出现当天的 db-*.sql.gz 与 .sha256，binlog/ 有已完结的 binlog
```

恢复演练照 `backup/restore-drill.md` 走一遍（验收第 4 条后半）。

## 12. 验收清单

| # | 验收 | 命令 | 期望 |
|---|---|---|---|
| 1 | HTTPS 可访问 | `curl -sS -o /dev/null -w '%{http_code}' https://<域名>/` | `200` |
| 1 | 任意路由刷新不 404 | `curl -sS -o /dev/null -w '%{http_code}' https://<域名>/orders` | `200`（SPA 回退） |
| 1 | `/api` 通到 Go | `curl -sS https://<域名>/api/auth/me` | `401` JSON（请求到了 Go；路径以实际实现为准） |
| 2 | 重启自动起 | `sudo reboot` 后 `systemctl is-active fulfillment-hub` | `active` |
| 2 | 主密钥不在环境 / 参数里 | §8 的两条 `grep` / `ps` | 无密钥；凭据目录里有文件 |
| 3 | Redis AOF 已开 | `redis-cli CONFIG GET appendonly appendfsync` | `yes` / `everysec` |
| 4 | 备份跑通 | §11 首跑 + 远端有文件 | journal 无错、远端齐全 |
| 4 | 恢复到指定时刻 | `backup/restore-drill.md` | §3 验收语句对得上 |

日常发布与回滚见 [release.md](release.md)。
