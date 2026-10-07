//go:build tools

// Package tools 只做依赖预置，不放任何运行代码（Go 官方 tools.go 惯例：
// 让 go mod tidy 知道这个依赖是「有意引入」的，不会被当未使用删掉）。
//
// 总纲 §13.1 / 子 spec §3.1 要求 go.mod 预置 S1 全部依赖（后端 ADR 所列各库），
// 而 shopspring/decimal 的第一个使用方是 S1-D（金额模型，总纲 §6「金额 DECIMAL」），
// 故在此钉为直接依赖。
package tools

import (
	_ "github.com/shopspring/decimal"
)
