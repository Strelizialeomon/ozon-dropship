//go:build integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"github.com/gin-gonic/gin"
)

// 回归：写接口 panic 也要留审计（旧行为：记录写在 c.Next() 之后，panic 直接跳过）。
func TestAuditWriteRecordsPanicHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := snowflake.Init(1); err != nil {
		t.Fatal(err)
	}
	db := testutil.MySQL(t)
	testutil.Truncate(t, db, "audit_logs")
	rec := audit.New(db)

	r := gin.New()
	r.Use(Recovery(), AuditWrite(rec))
	r.POST("/api/boom", func(_ *gin.Context) { panic("炸了") })

	req := httptest.NewRequest(http.MethodPost, "/api/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 应回 500，实际 %d", w.Code)
	}
	var n int64
	db.Model(&audit.AuditLog{}).Where("action = ?", "post /api/boom").Count(&n)
	if n != 1 {
		t.Fatalf("panic 的写操作也应留 1 条审计，实际 %d", n)
	}
	var row audit.AuditLog
	if err := db.Where("action = ?", "post /api/boom").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(row.Detail, `"panicked":true`) {
		t.Fatalf("审计 detail 应标注 panicked: %s", row.Detail)
	}
}
