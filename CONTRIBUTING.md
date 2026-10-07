# 参与贡献

欢迎 issue 和 PR。这份文件讲清楚：环境怎么搭、命令怎么跑、提交有什么习惯。

## 环境

| 端 | 需要 |
|---|---|
| `backend/` | Go 1.27+（go.mod 写死版本，`go` 命令会自动切工具链）、MySQL 8.x、Redis 7.x |
| `frontend/` | [Bun](https://bun.sh) |
| 工具 | [goose](https://github.com/pressly/goose)（数据库迁移，`go install github.com/pressly/goose/v3/cmd/goose@latest`）、[golangci-lint](https://golangci-lint.run/welcome/install/)（代码风格检查） |

## 后端（`backend/`）

```bash
cd backend
cp config/config.example.yaml config/config.yaml   # 按本机改数据库账号 / 口令等
mysql -uroot -e 'CREATE DATABASE IF NOT EXISTS ozon_dropship CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci'   # 首次建库
goose -dir migrations mysql "root@tcp(127.0.0.1:3306)/ozon_dropship?parseTime=true&multiStatements=true" up      # DSN 的账号 / 口令按本机改，与 config.yaml 一致
go run ./cmd/api                                   # 起服务（HTTP 与任务 worker 同进程）
go test ./...                                      # 跑测试（默认不需要外部服务）
golangci-lint run                                  # 代码风格（配置在 .golangci.yml）
```

建表用 [goose](https://github.com/pressly/goose) 手写 SQL 迁移（`backend/migrations/`），**没有 AutoMigrate**——上面的「建库 + `goose up`」两步都要有，表才存在；生产环境的安装与用法见 [deploy/install.md](deploy/install.md) §7。

带 `integration` / `smoke` build tag 的测试要连本地 MySQL / Redis（`go test ./...` 默认不跑它们），跑法见 [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md)。

## 前端（`frontend/`）

```bash
cd frontend
bun install
bun run dev        # 本地开发
bun test           # 测试
bun run typecheck  # 类型检查
bun run build      # 构建
bun run fmt:check  # 格式检查（dprint）
```

## 提交与 PR

- 提交信息用中文，带 conventional 前缀（`feat:` / `fix:` / `docs:` / `chore:` …）。
- 一个 PR 只做一件事；描述里写清「做了什么、为什么」。
- 开 PR 前跑一遍：后端 `go build ./... && go test ./...`；前端 `bun run typecheck && bun test`。
- 大改动先开 issue 对齐方向，再动手。

## 授权

提交即表示你同意自己的贡献以 [AGPL-3.0](LICENSE) 授权发布。
