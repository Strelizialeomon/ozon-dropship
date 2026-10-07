// Package logger 提供全进程统一的 slog JSON 日志（spec-s1a-foundation §6：slog、JSON 输出）。
// 日志写 stdout，由 systemd 收进 journal；不落文件、不做轮转（部署 ADR：systemd 托管）。
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	mu  sync.RWMutex
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
)

// Init 按 server.mode 设置日志级别：debug 段输出 DEBUG，release 段只到 INFO。
// 装配序列里必须早于一切组件调用（spec-s1a-foundation §3：配置 → 日志 → DB → …）。
func Init(mode string) {
	level := slog.LevelInfo
	if strings.EqualFold(mode, "debug") {
		level = slog.LevelDebug
	}
	mu.Lock()
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	mu.Unlock()
	slog.SetDefault(L())
}

// L 返回当前 logger；需要结构化字段时用它，别用 Infof 拼串。
func L() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return log
}

func Infof(format string, args ...any)  { L().Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { L().Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { L().Error(fmt.Sprintf(format, args...)) }

// Fatalf 记一条 error 后退出；只用于启动期资源缺失（配置错、库连不上），业务路径禁用。
func Fatalf(format string, args ...any) {
	L().Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// Sync 对齐标准档装配序列的收尾调用；stdout 无需刷盘，留作占位。
func Sync() {}
