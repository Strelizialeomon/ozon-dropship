# release —— 发布说明（后端二进制与前端静态资源分开发布）

> ADR 决定：前端构建产物不打包进 Go。两件产物各自发布、各自回滚，互不牵连。
> 首次安装不在本文（见 [install.md](install.md)）。`<server>` 换成服务器地址。

## 1. 后端发布（Go 二进制）

```bash
# ① 构建（仓库 backend/ 目录；服务器是 ARM 就改 GOARCH=arm64）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/hub-api ./cmd/api

# ② 有迁移变更时：先传迁移、跑 goose up（命令见 install.md §7），再重启

# ③ 上传并切换（保留上一个二进制做回滚）
scp /tmp/hub-api <server>:/opt/fulfillment-hub/api.new
ssh <server> 'cd /opt/fulfillment-hub && mv -f api api.prev 2>/dev/null; mv api.new api && systemctl restart fulfillment-hub'

# ④ 验证
ssh <server> 'systemctl status fulfillment-hub --no-pager; journalctl -u fulfillment-hub -n 50 --no-pager'
curl -sS -o /dev/null -w '%{http_code}\n' https://<域名>/api/auth/me   # 401 = 到达 Go
```

## 2. 前端发布（静态资源；不重启任何服务）

```bash
# ① 构建（仓库 frontend/ 目录）
bun install --frozen-lockfile && bun run build

# ② 服务器上留旧版快照（回滚用）
ssh <server> 'tar czf /srv/fulfillment-hub/web-prev.tgz -C /srv/fulfillment-hub/web .'

# ③ 同步（--delete 只作用于 web/ 目录内部，删的是旧构建的残留文件）
rsync -a --delete frontend/dist/ <server>:/srv/fulfillment-hub/web/

# ④ 验证：浏览器打开 https://<域名>/，再随手挑一个深层路由刷新（不 404）
```

## 3. 回滚

- **后端**：`ssh <server> 'cd /opt/fulfillment-hub && mv api api.bad && mv api.prev api && systemctl restart fulfillment-hub'`
  （`api.bad` 先留着对证）。**迁移回退单独评估**——`goose down` 会真的改表，先看清迁移内容再动。
- **前端**：`ssh <server> 'tar xzf /srv/fulfillment-hub/web-prev.tgz -C /srv/fulfillment-hub/web'`

## 4. 发布检查清单

- [ ] 构建通过（后端 `go build`；前端 `bun run build`）
- [ ] 有迁移就跑了 goose up，且看到预期版本号
- [ ] `systemctl status fulfillment-hub` 为 active、journal 无报错
- [ ] HTTPS 首页 200、深层路由刷新 200、`/api` 返 401（到达 Go）
- [ ] 备份定时器还在：`systemctl list-timers fulfillment-hub-backup`
