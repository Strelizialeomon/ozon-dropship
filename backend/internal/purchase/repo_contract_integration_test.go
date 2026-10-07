//go:build integration

package purchase

import (
	"context"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"
)

// ListTasks 空结果必须返回非 nil 切片（JSON 出 "items":[]，不是 null）——
// 前端 DataTable 拿到 null 会对它读 .length 直接抛错、整树卸载（2026-10-07 实报白屏）。
func TestListTasks_EmptyResult_NonNilSlice(t *testing.T) {
	db := testutil.MySQL(t)
	repo := NewRepo(db)

	rows, total, err := repo.ListTasks(context.Background(), TaskFilter{StoreID: "no-such-store"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 0 {
		t.Fatalf("期望 0 条，得 %d", total)
	}
	if rows == nil {
		t.Fatal(`空结果返回 nil 切片：JSON 会出 "items":null，前端会白屏；应返回 []`)
	}
}
