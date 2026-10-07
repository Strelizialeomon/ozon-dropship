# 恢复演练 —— 恢复到指定时刻

> 验收第 4 条要跑的演练。规则（ADR）：每月演练一次；恢复链 = 每日全量 dump + binlog（两端各留 30 天）。
> binlog 回放要「干净可复现」，演练一律在**演练库或演练机**上做，别碰生产库。

## 0. 恢复链与前提

| 件 | 在哪 | 干什么 |
|---|---|---|
| `db-<时间戳>.sql.gz` + `.sha256` | 备份机 / 对象存储的 `db/` | 某天 03:30 的全量 |
| `binlog.0000xx` 文件 | 备份机的 `binlog/` | 全量之后、到目标时刻之间的每次写入 |
| 主密钥（base64 那串） | **密码管理器（离机）** | 解数据库里的凭据密文；丢了就只能全店重新录入密钥（见 §4） |

先确认要恢复到的**目标时刻 T**（比如「昨天 12:00」），再挑 T 之前最近的一次全量。

**防呆（最重要的一步）**：下面所有 `mysql` / `mysqlbinlog … | mysql` 都要打到**演练实例**，不是生产实例。
先定一个指向演练实例的变量（演练机本机就是裸 `mysql`；同机的另一个实例或另一台机器，
加 `--socket=<演练实例 socket>` 或 `--host/--port`）：

```bash
DRILL_MYSQL="mysql"                                              # 演练机本机
# DRILL_MYSQL="mysql --socket=/run/mysqld/mysqld-drill.sock"     # 同机演练实例
$DRILL_MYSQL -e "SELECT @@port, @@socket, @@datadir;"            # ← 动手前先看这眼：指对了吗？
```

（dump 是 `--databases` 导出：自带 `CREATE DATABASE` + 每张表 `DROP TABLE IF EXISTS`。
打错实例 = 逐表覆盖、没有后悔药，所以这条防呆别省。）

## 1. 全量落库

```bash
# 确认 dump 没坏（在同时放着 db-*.sql.gz 与 .sha256 的目录里）：
sha256sum -c db-20261007-0330.sql.gz.sha256
# 校验通过后恢复：
zcat db-20261007-0330.sql.gz | $DRILL_MYSQL
```

## 2. 从 dump 头部取 binlog 坐标

```bash
zcat db-20261007-0330.sql.gz | head -80 | grep -m1 'CHANGE REPLICATION SOURCE'
# 形如：-- CHANGE REPLICATION SOURCE TO SOURCE_LOG_FILE='binlog.000123', SOURCE_LOG_POS=456;
# 记录：文件 binlog.000123，位置 456。
```

## 3. 回放 binlog 到 T

把 `binlog.000123` 起、到 T 所在的那个文件为止，按顺序列给 mysqlbinlog
（`--start-position` 只作用于第一个文件；`--stop-datetime` 按**运行 mysqlbinlog 那台机器的本地时区**解释）。

**用 root 跑回放**：行格式（8.x 默认）下回放执行的是 `BINLOG` 语句，要 `BINLOG_ADMIN`（或弃用的 SUPER）——
`hub_backup` 没这个权限，用它会在第一条行事件就 Access denied、回放半途而废。演练机上就是 `sudo`：

```bash
mysqlbinlog --start-position=456 \
  --stop-datetime='2026-10-07 12:00:00' \
  ./binlog/binlog.000123 ./binlog/binlog.000124 ./binlog/binlog.000125 | sudo $DRILL_MYSQL
```

演练验收：查一眼数据确实停在 T，而不是「脚本没报错就算过」——

```bash
$DRILL_MYSQL -e "SELECT MAX(created_at) FROM fulfillment_hub.orders; SELECT MAX(at) FROM fulfillment_hub.audit_logs;"
# 结果应 ≤ T；再抽查一两条 T 前后已知变化的数据，对得上才算过。
```

## 4. 灾难恢复（换机器）：别漏了主密钥

systemd 加密凭据**绑定生成它的那台机器**（TPM / 主机密钥），机器没了就解不开。
所以「整个恢复」= 三件事：

1. 新机器照 `install.md` 装到 §6 之前；
2. 从密码管理器拿出主密钥那串 base64 → `echo '<那串>' | base64 -d > /root/hub-master-key.plain` →
   在新机器上重新跑 `systemd-creds encrypt`（命令见 install.md §6）——**不需要**旧机器上的任何东西；
3. 按 §1–§3 恢复 DB，启动服务，验证店里已存的凭据能正常解密（操作台看一眼凭据尾号即可）。

> 主密钥离机备份是硬要求：只备份数据库、不备份主密钥，恢复出来也解不开凭据。
> 建议每次轮换主密钥时，顺手把新值更新进密码管理器。

## 5. 边界（如实标）

- **可恢复窗口 = 最近 30 天**（全量与 binlog 都保留 30 天，ADR 口径）。更早只能恢复到某次全量的时点，中间的空洞补不回来。
- **可恢复到的最新一端 = 最近一次成功备份跑完的时刻**（最坏丢约 24 小时，取决于 timer 频率）。
  备份脚本每轮会把 dump 的坐标文件补一次 `FLUSH LOGS` 关闭后外送，所以最新那份 dump 本身可以完整回放到它自己的时点；
  但它跑完之后的写入要等下一轮才外送。想收紧窗口：把 `fulfillment-hub-backup.timer` 的 `OnCalendar` 调密
  （比如每 6 小时），或改 `mysqlbinlog --stop-never` 连续外送。
- 回放期间如果有人在用旧库，坐标会漂——演练和真恢复都应在「不再写入」的库上做（生产真恢复时先停服务）。
- 本文件只覆盖 MySQL。Redis 里是任务队列与会话（总纲 §6），丢了由补投扫描和重新登录兜底，不进本演练。
