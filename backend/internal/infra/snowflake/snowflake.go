// Package snowflake 生成全局唯一 ID：模型主键用字符串形式的雪花 ID（标准档模型约定）。
package snowflake

import (
	"fmt"

	"github.com/bwmarrin/snowflake"
)

var node *snowflake.Node

// Init 初始化雪花节点。nodeID 0~1023；单机部署固定填 1。
func Init(nodeID int64) error {
	var err error
	node, err = snowflake.NewNode(nodeID)
	if err != nil {
		return fmt.Errorf("初始化雪花节点失败: %w", err)
	}
	return nil
}

// GenStringID 生成 Base10 字符串形式的雪花 ID（模型主键用）。
// 未 Init 直接 panic：启动期装配错误就该炸在启动时，不做懒初始化。
func GenStringID() string {
	if node == nil {
		panic("snowflake 未初始化，main 装配序列里先调 snowflake.Init")
	}
	return node.Generate().String()
}
