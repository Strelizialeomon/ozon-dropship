//go:build integration

package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 回归：软删行不再占唯一键——同名店删掉后可以重新建（旧行为：1062 永远建不出）。
func TestShopNameReusableAfterDelete(t *testing.T) {
	db, _ := setupStore(t)
	repo := NewRepo(db)
	ctx := context.Background()

	s1 := &Shop{ID: "shop-1", Name: "一号店", Mode: ModeRFBS, Currency: "CNY", Status: ShopStatusActive}
	if err := repo.Create(ctx, s1); err != nil {
		t.Fatalf("建店: %v", err)
	}
	if err := repo.SoftDelete(ctx, s1.ID); err != nil {
		t.Fatalf("删店: %v", err)
	}
	s2 := &Shop{ID: "shop-2", Name: "一号店", Mode: ModeFBP, Currency: "CNY", Status: ShopStatusActive}
	if err := repo.Create(ctx, s2); err != nil {
		t.Fatalf("软删后同名重建应成功，实际: %v", err)
	}
	// 但两个存活同名仍要被挡。
	s3 := &Shop{ID: "shop-3", Name: "一号店", Mode: ModeRFBS, Currency: "CNY", Status: ShopStatusActive}
	if err := repo.Create(ctx, s3); err == nil || !isDuplicate(err) {
		t.Fatalf("两个存活同名应当 1062，实际: %v", err)
	}
}

// 回归：对已软删的店做更新必须失败（不能把删掉的店「写活」）。
func TestUpdateSoftDeletedShopFails(t *testing.T) {
	db, _ := setupStore(t)
	repo := NewRepo(db)
	ctx := context.Background()

	s := &Shop{ID: "shop-x", Name: "要删的店", Mode: ModeRFBS, Currency: "CNY", Status: ShopStatusActive}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDelete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	s.Name = "改个名"
	if err := repo.Update(ctx, s); !errors.Is(err, ErrShopNotFound) {
		t.Fatalf("更新软删行应 ErrShopNotFound，实际: %v", err)
	}
	var alive bool
	db.Raw("SELECT del_flag = 0 FROM stores WHERE id = ?", s.ID).Scan(&alive)
	if alive {
		t.Fatal("软删行被写活了")
	}
}

// 回归：软删凭据不再挡同店同 kind 的重新录入（旧行为：1062 → 再叠加并发 500）。
func TestCredentialReusableAfterSoftDelete(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "first-token-1111"}, nil); err != nil {
		t.Fatalf("首次录入: %v", err)
	}
	// 手工软删（S1 还没有凭据删除接口）。
	if err := db.Exec("UPDATE credentials SET del_flag = 1 WHERE kind = ?", KindAlibabaToken).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "second-token-2222"}, nil); err != nil {
		t.Fatalf("软删后重新录入应成功，实际: %v", err)
	}
	got, err := svc.Get(ctx, "", KindAlibabaToken)
	if err != nil {
		t.Fatalf("读取新凭据: %v", err)
	}
	if got.Payload["access_token"] != "second-token-2222" {
		t.Fatalf("读到的应是新凭据，实际 %+v", got.Payload)
	}
}

// 回归：并发 Put（双击保存）不得 500；最终恰一行存活、内容是其中一次写入。
func TestConcurrentPutIsSafe(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	const n = 6
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Put(ctx, "", KindAlibabaApp, map[string]string{
				"app_key":    "key-" + string(rune('a'+i)),
				"app_secret": "secret-" + string(rune('a'+i)),
			}, nil)
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("并发 Put 不应报错（曾被 1062 撞成 500），实际: %v", err)
		}
	}
	var live int64
	db.Model(&Credential{}).Where("kind = ? AND del_flag = ?", KindAlibabaApp, false).Count(&live)
	if live != 1 {
		t.Fatalf("同店同 kind 存活行应恰 1 行，实际 %d", live)
	}
	if _, err := svc.Get(ctx, "", KindAlibabaApp); err != nil {
		t.Fatalf("并发写完后应能正常解密读取: %v", err)
	}
}

// 回归：轮换不传 expires_at 不得把原到期时间抹成 NULL（抹掉 = 该凭据从此不再告警）。
func TestRotateWithoutExpiryKeepsExpiresAt(t *testing.T) {
	db, svc := setupStore(t)
	ctx := context.Background()

	exp := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Millisecond)
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "tok-a-1111"}, &exp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "tok-b-2222"}, nil); err != nil {
		t.Fatal(err)
	}
	var row Credential
	if err := db.Where("kind = ?", KindAlibabaToken).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt == nil {
		t.Fatal("轮换未传 expires_at 时原到期时间被抹掉了")
	}
	if !row.ExpiresAt.Equal(exp) {
		t.Fatalf("到期时间不应被改动：got %v want %v", row.ExpiresAt, exp)
	}
	// 显式传新到期时间 → 更新且复位告警档。
	newExp := time.Now().UTC().Add(10 * 24 * time.Hour).Truncate(time.Millisecond)
	if _, err := svc.Put(ctx, "", KindAlibabaToken, map[string]string{"access_token": "tok-c-3333"}, &newExp); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("kind = ?", KindAlibabaToken).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !row.ExpiresAt.Equal(newExp) {
		t.Fatalf("显式传的到期时间应生效：got %v want %v", row.ExpiresAt, newExp)
	}
}
