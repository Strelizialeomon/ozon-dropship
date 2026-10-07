// Package testutil 是集成测试的公共助手：真 MySQL / 真 Redis / 真保险箱。
//
// 集成测试用构建标签隔离，默认 go test ./... 不跑（不需要外部依赖即可全绿）：
//
//	TEST_MYSQL_DSN='root:devroot@tcp(127.0.0.1:3307)/ozon_dropship_test?parseTime=true&loc=UTC' \
//	TEST_REDIS_ADDR=127.0.0.1:6390 \
//	go test -tags integration -p 1 ./...
//
// ⚠️ -p 1：多个测试包共享同一个库，串行跑避免互相 TRUNCATE。
package testutil

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/vault"

	_ "github.com/go-sql-driver/mysql" // database/sql 直连（goose 用）
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 环境变量名。
const (
	EnvMySQLDSN = "TEST_MYSQL_DSN"
	EnvRedis    = "TEST_REDIS_ADDR"
)

// Tables 全部 13 张业务表（TRUNCATE 用）。
var Tables = []string{
	"stores", "relay_points", "credentials", "supplier_offers", "offer_links",
	"orders", "order_items", "purchase_tasks", "purchase_orders", "shipments",
	"exceptions", "audit_logs", "users",
}

var (
	mysqlOnce sync.Once
	mysqlErr  error
)

// MySQL 连测试库并确保迁移已跑（goose up，跨进程用 MySQL 命名锁串行化）。
// env 未配置 → t.Skip。
func MySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(EnvMySQLDSN)
	if dsn == "" {
		t.Skipf("%s 未设置，跳过集成测试", EnvMySQLDSN)
	}
	mysqlOnce.Do(func() { mysqlErr = ensureMigrations(dsn) })
	if mysqlErr != nil {
		t.Fatalf("准备测试库失败: %v", mysqlErr)
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	return db
}

// ensureMigrations 用命名锁护住 goose up：多个测试二进制可能同时开工。
func ensureMigrations(dsn string) error {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var got int
	if err := sqlDB.QueryRowContext(ctx, "SELECT GET_LOCK('ozon_dropship_test_migrate', 25)").Scan(&got); err != nil {
		return err
	}
	defer func() {
		_, _ = sqlDB.ExecContext(context.Background(), "DO RELEASE_LOCK('ozon_dropship_test_migrate')")
	}()

	dir, err := LocateMigrations()
	if err != nil {
		return err
	}
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	return goose.Up(sqlDB, dir)
}

// LocateMigrations 从当前工作目录向上找到 backend/migrations。
func LocateMigrations() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		cand := filepath.Join(dir, "migrations")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// Redis 连测试 Redis；env 未配置 → t.Skip。库号固定 15，跟开发库隔开。
func Redis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv(EnvRedis)
	if addr == "" {
		t.Skipf("%s 未设置，跳过集成测试", EnvRedis)
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("连接测试 Redis 失败: %v", err)
	}
	t.Cleanup(func() {
		// 清本库号的数据，别把上一个测试的会话/队列留给下一个。
		_ = client.FlushDB(context.Background()).Err()
		_ = client.Close()
	})
	return client
}

// Truncate 清空指定表（默认全部 13 张）。
func Truncate(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	if len(tables) == 0 {
		tables = Tables
	}
	for _, tb := range tables {
		if err := db.Exec("TRUNCATE TABLE `" + tb + "`").Error; err != nil {
			t.Fatalf("TRUNCATE %s 失败: %v", tb, err)
		}
	}
}

// Vault 临时目录里生成一份 keyset 的可用保险箱。
func Vault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	ks, err := vault.GenerateKeysetJSON()
	if err != nil {
		t.Fatalf("生成 keyset 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault_keyset.json"), ks, 0o600); err != nil {
		t.Fatalf("写 keyset 失败: %v", err)
	}
	v, err := vault.New(dir, "vault_keyset.json")
	if err != nil {
		t.Fatalf("构造保险箱失败: %v", err)
	}
	if !v.Enabled() {
		t.Fatal("保险箱应处于启用状态")
	}
	return v
}
