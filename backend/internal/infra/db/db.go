// Package db 管理全局 MySQL 连接（GORM 单例）。
//
// 本项目不用 GORM AutoMigrate（生效 ADR：goose 手写 SQL 迁移，
// 见 docs/decisions/2026-10-07-backend-stack.md）——本包只管连接与连接池。
package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/logger"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB 全局数据库连接实例（单例）。
var DB *gorm.DB

const connectTimeout = 30 * time.Second

// Init 建立 MySQL 连接并设置连接池；失败返回 error 由调用方决定退出。
func Init(dsn string) error {
	var err error
	DB, err = connectWithTimeout(dsn)
	if err != nil {
		return err
	}
	logger.L().Info("database initialized")
	return nil
}

// Close 关闭底层连接池（优雅退出收尾用）。
func Close() error {
	if DB == nil {
		return nil
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func connectWithTimeout(dsn string) (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	gormLog := gormlogger.New(slogWriter{}, gormlogger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})

	type result struct {
		db  *gorm.DB
		err error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
			PrepareStmt: true,
			Logger:      gormLog,
			// TranslateError：把 MySQL 1062 等驱动错误翻译成 gorm.ErrDuplicatedKey，
			// 业务层据它返回「重复」而不是 500。
			TranslateError: true,
			// NowFunc：CreatedAt/UpdatedAt 等自动时间戳一律按 UTC 生成
			// （总纲 §6：时间按 UTC 存；不统一的话，响应里的时间会一半本地时区一半 UTC）。
			NowFunc: func() time.Time { return time.Now().UTC() },
		})
		ch <- result{conn, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("数据库连接超时（%s）", connectTimeout)
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("数据库连接失败: %w", r.err)
		}
		sqlDB, err := r.db.DB()
		if err != nil {
			return nil, fmt.Errorf("获取底层 sql.DB 失败: %w", err)
		}
		sqlDB.SetMaxOpenConns(20)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(time.Hour)
		if err := sqlDB.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("数据库连通性检查失败: %w", err)
		}
		return r.db, nil
	}
}

// slogWriter 把 GORM 的慢查询 / 错误日志引到 slog（stdout JSON）。
type slogWriter struct{}

func (slogWriter) Printf(format string, args ...any) {
	logger.Errorf(strings.TrimSuffix(strings.TrimSpace(format), "\n"), args...)
}
