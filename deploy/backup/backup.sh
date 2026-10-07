#!/usr/bin/env bash
# 履约中台 —— 每日备份
# 安装：install -m 0755 deploy/backup/backup.sh /usr/local/bin/hub-backup（见 install.md §11）
# 做什么：mysqldump 全量（--single-transaction 一致性快照 + --source-data=2 记 binlog 坐标
#          + --flush-logs 轮转日志）→ 连同已完结的 binlog 文件外送到另一台机器或对象存储 →
#          两端各保留 RETENTION_DAYS 天（默认 30 天，与 ADR 一致）。
# 配置：/etc/fulfillment-hub/backup.env（照 backup.env.example 填）
# 恢复：见 restore-drill.md
set -euo pipefail

CONF=${BACKUP_CONF:-/etc/fulfillment-hub/backup.env}
LOG_PREFIX="[hub-backup $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

log() { echo "$LOG_PREFIX $*"; }
die() { echo "$LOG_PREFIX 出错：$*" >&2; exit 1; }

[ -r "$CONF" ] || die "缺配置文件 ${CONF}（照 deploy/backup/backup.env.example 安装）"
# shellcheck source=/dev/null
. "$CONF"

: "${MYSQL_CNF:?backup.env 里要设 MYSQL_CNF}" \
  "${DB_NAME:?backup.env 里要设 DB_NAME}" \
  "${BINLOG_DIR:?backup.env 里要设 BINLOG_DIR（PITR 必需）}"
LOCAL_DIR=${LOCAL_DIR:-/var/backups/fulfillment-hub}
RETENTION_DAYS=${RETENTION_DAYS:-30}

# ssh/rsync 的超时与保活：防「连接被黑洞后进程挂死、flock 锁不释放、备份从此静默停摆」。
# BatchMode=yes：非交互，连不上就失败退出，不许弹密码。unit 另有 TimeoutStartSec 兜底。
SSH_OPTS="-o ConnectTimeout=10 -o ServerAliveInterval=30 -o ServerAliveCountMax=4 -o BatchMode=yes"

if [ -n "${RSYNC_REMOTE:-}" ] && [ -n "${RCLONE_REMOTE:-}" ]; then
  die "RSYNC_REMOTE 与 RCLONE_REMOTE 只能设一个（二选一，另一个留空）"
fi
if [ -z "${RSYNC_REMOTE:-}" ] && [ -z "${RCLONE_REMOTE:-}" ]; then
  die "RSYNC_REMOTE（另一台机器）与 RCLONE_REMOTE（对象存储）必须设一个"
fi
[ -r "$MYSQL_CNF" ] || die "读不到 $MYSQL_CNF"
[ -d "$BINLOG_DIR" ] || die "binlog 目录不存在：${BINLOG_DIR}（没开 binlog？见 deploy/mysql/fulfillment-hub.cnf）"

# 不许叠跑（手动跑撞上定时器时，后到的一个直接失败，journal 可见）
exec 9>/var/lock/fulfillment-hub-backup.lock
flock -n 9 || die "已有一次备份在跑（锁 /var/lock/fulfillment-hub-backup.lock）"

mkdir -p "$LOCAL_DIR/db"
TS=$(date +%Y%m%d-%H%M%S)
DUMP_GZ="$LOCAL_DIR/db/db-$TS.sql.gz"

# ── 1. 全量 dump ────────────────────────────────────────────────
# --source-data=2：在文件头部以注释记录 binlog 坐标（PITR 的起点）
# --flush-logs：  轮转 binlog，让坐标落在文件边界上，回放时文件齐全
log "开始 mysqldump → $DUMP_GZ"
mysqldump --defaults-extra-file="$MYSQL_CNF" \
  --single-transaction --source-data=2 --flush-logs \
  --databases "$DB_NAME" | gzip > "$DUMP_GZ"
gzip -t "$DUMP_GZ" || die "dump 不是完整的 gzip，别外送"
[ -s "$DUMP_GZ" ] || die "dump 是空文件"
# 校验和只写文件名（不带本地路径），这样它跟着 dump 到了备份机也能 `sha256sum -c`
( cd "$LOCAL_DIR/db" && sha256sum "$(basename "$DUMP_GZ")" ) > "$DUMP_GZ.sha256"
log "dump 完成：$(du -h "$DUMP_GZ" | cut -f1)"

# ── 1.5 关闭 dump 的坐标文件，保证「最新 dump 的起点」也在本轮外送 ──
# mysqldump 的 --flush-logs 让坐标落在它刚新建的那个文件上。若不补这一次 FLUSH LOGS，
# 该文件就是下面要排除的「当前文件」，会被永久落下——最新一天的 dump 到了备份机也回放不了。
mysql --defaults-extra-file="$MYSQL_CNF" -e "FLUSH LOGS"

# ── 2. 找当前的 binlog 文件：外送时排除（写一半的文件不算备份）──────
# 到这里，dump 的坐标文件已在 1.5 被关闭、随本轮外送；被排除的是刚生成的这一个。
CURRENT_BINLOG=$(mysql --defaults-extra-file="$MYSQL_CNF" --batch --skip-column-names \
  -e "SHOW BINARY LOG STATUS" | awk 'NR==1{print $1}')
[ -n "$CURRENT_BINLOG" ] || die "拿不到当前 binlog 文件名（账号缺 REPLICATION CLIENT 权限？）"
log "当前 binlog：${CURRENT_BINLOG}（本轮不外送；已关闭的文件都随本轮外送）"

# ── 3. 外送 ────────────────────────────────────────────────────
if [ -n "${RSYNC_REMOTE:-}" ]; then
  RHOST=${RSYNC_REMOTE%%:*}
  RPATH=${RSYNC_REMOTE#*:}
  ssh $SSH_OPTS "$RHOST" "mkdir -p '$RPATH/db' '$RPATH/binlog'"
  log "rsync → $RSYNC_REMOTE"
  rsync -a --timeout=600 -e "ssh $SSH_OPTS" "$DUMP_GZ" "$DUMP_GZ.sha256" "$RSYNC_REMOTE/db/"
  # 只拷 binlog 文件（datadir 与 binlog 同目录时，别把整个数据目录拖走）；
  # rsync 规则按命令行顺序先匹配先算：先排除正在写的那个（写一半的不算备份），再放行 binlog.*，其余全挡
  rsync -a --timeout=600 -e "ssh $SSH_OPTS" \
    --exclude "$CURRENT_BINLOG" --include='binlog.[0-9]*' --exclude='*' \
    "$BINLOG_DIR/" "$RSYNC_REMOTE/binlog/"

  # ── 4. 两端按保留期清理（远端不加 rsync --delete：本地坏了不能反过来清远端）──
  log "清理本地 > $RETENTION_DAYS 天的 dump"
  find "$LOCAL_DIR/db" -name 'db-*.sql.gz*' -type f -mtime "+$RETENTION_DAYS" -delete
  log "清理远端 > $RETENTION_DAYS 天"
  ssh $SSH_OPTS "$RHOST" "find '$RPATH/db' -type f -mtime +$RETENTION_DAYS -delete; \
                find '$RPATH/binlog' -type f -mtime +$RETENTION_DAYS -delete"
else
  log "rclone → $RCLONE_REMOTE"
  rclone copy "$DUMP_GZ" "$RCLONE_REMOTE/db/" --checksum
  rclone copy "$DUMP_GZ.sha256" "$RCLONE_REMOTE/db/"
  # 同样只拷 binlog 文件。rclone 的 --include/--exclude 会按类型重排优先级（include 恒在前），
  # 混用会把「正在写的那个」也带进去——照官方口径改用 --filter 显式定序：先排除在写的，
  # 再放行 binlog.*，其余全挡（规则先匹配先算，顺序即优先级）
  rclone copy "$BINLOG_DIR" "$RCLONE_REMOTE/binlog/" \
    --filter "- $CURRENT_BINLOG" \
    --filter '+ binlog.[0-9]*' \
    --filter '- *'

  log "清理本地 > $RETENTION_DAYS 天的 dump"
  find "$LOCAL_DIR/db" -name 'db-*.sql.gz*' -type f -mtime "+$RETENTION_DAYS" -delete
  log "清理远端 > $RETENTION_DAYS 天"
  rclone delete --min-age "${RETENTION_DAYS}d" "$RCLONE_REMOTE/db"
  rclone delete --min-age "${RETENTION_DAYS}d" "$RCLONE_REMOTE/binlog"
fi

# 顺带一提：本机 binlog 自身按 MySQL 的 binlog_expire_logs_seconds 保留 30 天，
# 远端仓的对应文件靠上面的 mtime 清理；两边窗口一致。
log "备份完成"
