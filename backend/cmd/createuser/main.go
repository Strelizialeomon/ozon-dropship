// createuser 建操作台用户（首个管理员用；照 xhs-analysis 的 cmd 形态）。
//
//	go run ./cmd/createuser -name admin -role admin
//	（密码不走命令行历史：交互式终端会无回显提示，管道则读 stdin 一行）
//
//	echo '你的密码' | go run ./cmd/createuser -name admin
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/auth"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/config"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/db"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/middleware"

	"golang.org/x/term"
)

func main() {
	configPath := flag.String("config", "config", "配置目录路径")
	name := flag.String("name", "", "登录名（必填）")
	role := flag.String("role", middleware.RoleAdmin, "角色 admin / operator")
	password := flag.String("password", "", "密码；不传则交互式输入或从 stdin 读")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "用法: createuser -name <登录名> [-role admin|operator] [-config <配置目录>]")
		os.Exit(2)
	}
	if *role != middleware.RoleAdmin && *role != middleware.RoleOperator {
		fmt.Fprintln(os.Stderr, "role 只能是 admin 或 operator")
		os.Exit(2)
	}

	pw, err := readPassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取密码失败: %v\n", err)
		os.Exit(1)
	}
	if len(pw) < 8 {
		fmt.Fprintln(os.Stderr, "密码至少 8 位")
		os.Exit(2)
	}

	if err := config.Init(*configPath); err != nil {
		panic(err)
	}
	cfg := config.Get()
	logger.Init("release")
	defer logger.Sync()

	if err := snowflake.Init(1); err != nil {
		logger.Fatalf("[createuser] %v", err)
	}
	if err := db.Init(cfg.Database.DSN()); err != nil {
		logger.Fatalf("[createuser] %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	repo := auth.NewRepo(db.DB)
	if _, err := repo.ByName(ctx, *name); err == nil {
		logger.Fatalf("[createuser] 用户 %s 已存在", *name)
	}

	hash, err := auth.HashPassword(pw)
	if err != nil {
		logger.Fatalf("[createuser] 生成密码哈希失败: %v", err)
	}
	u := &auth.User{
		ID:           snowflake.GenStringID(),
		Name:         *name,
		PasswordHash: hash,
		Role:         *role,
		Status:       auth.StatusActive,
	}
	if err := repo.Create(ctx, u); err != nil {
		logger.Fatalf("[createuser] 建用户失败: %v", err)
	}
	fmt.Printf("✅ 已创建 %s（角色 %s，ID %s）\n", u.Name, u.Role, u.ID)
}

// readPassword 依次尝试：-password 参数 → 交互式终端无回显 → stdin 一行（管道）。
func readPassword(fromFlag string) (string, error) {
	if fromFlag != "" {
		return fromFlag, nil
	}
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "请输入密码（不回显）: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text()), nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("没有可用输入（-password / 终端 / 管道至少给一个）")
}
