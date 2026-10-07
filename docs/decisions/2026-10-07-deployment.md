# ADR-20261007-deployment：单机部署、不用 Docker；Caddy 管 HTTPS 与前端静态资源，systemd 托管 Go

- 状态：生效中
- 日期：2026-10-07
- 决策人：用户（owner）
- 适用范围：部署、运维、备份
- 取代：无
- 被取代：无
- 决策来源：spec v1.0 头部（单机部署 + 同进程 worker）；2026-10-07 会话（「直接单应用部署，不要 docker」「我们会使用 caddy」「前端单独作为静态资源部署，不打包进入 go」；备份选择卡）

## 决定

- 一台 Linux 服务器，**不用 Docker**。
- Go 程序编成二进制，systemd 托管；asynq worker 与 HTTP 服务同进程。
- **Caddy** 负责自动 HTTPS、托管前端静态资源、把 `/api/*` 与推送入口转给 Go。
- 前端构建产物作为静态资源单独部署，**不打包进 Go 程序**。
- MySQL 8.4 与 Redis 直接装在主机上；Redis 开 AOF。
- 主密钥用 systemd 加密凭据注入，不放环境变量。
- 备份：每天 mysqldump 全量传到另一台机器或对象存储 + 保留 binlog（可恢复到任意时刻）；每月演练一次恢复。

## 依据

- 自用系统、日 200–1000 单，单机足够；少一层容器、排障直接。
- Ozon 推送需要公网 HTTPS；Caddy 自动申请和续期证书。

## 否决了什么

- Docker / Docker Compose；多机或 K8s；拆独立 worker 进程。
- Go 程序自己管证书；前端打包进 Go 程序；单独放 Nginx。
- 只拍云盘快照的备份方式。

## 接受的代价

- 单点故障：机器挂了整套停；
- MySQL、Redis、Caddy 都要手工安装和升级；
- 服务器必须有固定公网 IP 和域名。

## 允许重开的条件

- 单机撑不住业务量，或业务要求高可用；
- 运维人员增加，需要统一的容器化交付。

## 关联

- [spec-fulfillment-hub](../specs/spec-fulfillment-hub.md) §4.1、§5.5
