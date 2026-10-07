//go:build integration

package store

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/audit"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/snowflake"
	"github.com/Strelizialeomon/ozon-dropship/backend/internal/testutil"

	"gorm.io/gorm"
)

func setupStore(t *testing.T) (*gorm.DB, *CredentialService) {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake: %v", err)
	}
	db := testutil.MySQL(t)
	testutil.Truncate(t, db)
	v := testutil.Vault(t)
	rec := audit.New(db)
	svc := NewCredentialService(db, v, rec)
	return db, svc
}

// 验收：库里是密文；读回来是明文；移动密文到别的行解不开。
func TestCredentialEncryptedAtRestAndAADBound(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	payload := map[string]string{"access_token": "tok-abcdefg", "refresh_token": "ref-123"}
	if _, err := svc.Put(ctx, "", KindAlibabaToken, payload, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 1) 库里是密文：secret_enc 不含明文片段。
	var row Credential
	if err := db.Where("kind = ?", KindAlibabaToken).First(&row).Error; err != nil {
		t.Fatalf("查行失败: %v", err)
	}
	if bytes.Contains(row.SecretEnc, []byte("tok-abcdefg")) {
		t.Fatal("secret_enc 里出现了明文")
	}
	var hits int64
	db.Raw("SELECT COUNT(*) FROM credentials WHERE secret_enc LIKE ?", "%tok-abcdefg%").Scan(&hits)
	if hits != 0 {
		t.Fatal("SQL 层能搜到明文")
	}

	// 2) 读回明文。
	got, err := svc.Get(ctx, "", KindAlibabaToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Payload["access_token"] != "tok-abcdefg" || got.Payload["refresh_token"] != "ref-123" {
		t.Fatalf("载荷不一致: %+v", got.Payload)
	}

	// 3) 密文挪到别的行解不开：复制 secret_enc 到新行（新行 ID 不同 → AAD 不匹配）。
	moved := Credential{
		ID:        snowflake.GenStringID(),
		Kind:      KindOzonAPIKey,
		SecretEnc: row.SecretEnc, // 原样搬运
		MaskedTail: "xxxx",
	}
	if err := db.Create(&moved).Error; err != nil {
		t.Fatalf("插入搬运行失败: %v", err)
	}
	if _, err := svc.Get(ctx, "", KindOzonAPIKey); err == nil {
		t.Fatal("密文挪到别的行应解不开")
	} else if !strings.Contains(err.Error(), "解密") {
		t.Fatalf("应当是解密失败，得到: %v", err)
	}
}

// 验收：接口只返回脱敏尾号；读取与轮换写审计。
func TestCredentialListMaskedAndAudited(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "verysecret-1234"}, nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	metas, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(metas))
	}
	if metas[0].Masked != "****1234" {
		t.Fatalf("脱敏尾号不对: %q", metas[0].Masked)
	}

	// 读取（Get）写审计。
	if _, err := svc.Get(ctx, "", KindAlibabaToken); err != nil {
		t.Fatalf("Get: %v", err)
	}
	countAudit := func(action string) int64 {
		var n int64
		db.Model(&audit.AuditLog{}).Where("action = ?", action).Count(&n)
		return n
	}
	if countAudit("credential.read") != 1 {
		t.Fatalf("应有 1 条 credential.read 审计")
	}

	// 轮换（同店同 kind 覆盖）写 credential.rotate，且尾号更新。
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "rotated-xyz-9876"}, nil); err != nil {
		t.Fatalf("轮换: %v", err)
	}
	if countAudit("credential.rotate") != 1 {
		t.Fatalf("应有 1 条 credential.rotate 审计")
	}
	metas, _ = svc.List(ctx, nil)
	if metas[0].Masked != "****9876" {
		t.Fatalf("轮换后尾号应更新，实际 %q", metas[0].Masked)
	}
	if metas[0].RotatedAt == nil {
		t.Fatal("轮换后应有 rotated_at")
	}

	// 企业级凭据：同 kind 至多一行（唯一键兜底）。
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "again-0000"}, nil); err != nil {
		t.Fatalf("覆盖应成功: %v", err)
	}
	var n int64
	db.Model(&Credential{}).Where("kind = ?", KindAlibabaToken).Count(&n)
	if n != 1 {
		t.Fatalf("企业级凭据同 kind 只应 1 行，实际 %d", n)
	}
}
