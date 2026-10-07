package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	dir := t.TempDir()
	ks, err := GenerateKeysetJSON()
	if err != nil {
		t.Fatalf("GenerateKeysetJSON: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "k.json"), ks, 0o600); err != nil {
		t.Fatalf("write keyset: %v", err)
	}
	v, err := New(dir, "k.json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	v := newTestVault(t)
	aad := AADFor("credentials", "123")
	ct, err := v.Encrypt([]byte(`{"api_key":"secret"}`), aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ct, []byte("secret")) {
		t.Fatal("密文里出现了明文")
	}
	pt, err := v.Decrypt(ct, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(pt) != `{"api_key":"secret"}` {
		t.Fatalf("roundtrip 不一致: %s", pt)
	}
}

// 密文挪到别的行解不开（AAD 绑定表名 + 行 ID）。
func TestDecryptWithWrongAADFails(t *testing.T) {
	v := newTestVault(t)
	ct, err := v.Encrypt([]byte("hello"), AADFor("credentials", "123"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := v.Decrypt(ct, AADFor("credentials", "456")); err == nil {
		t.Fatal("换了行 ID 应该解密失败")
	}
	if _, err := v.Decrypt(ct, AADFor("orders", "123")); err == nil {
		t.Fatal("换了表名应该解密失败")
	}
}

func TestDisabledVault(t *testing.T) {
	v, err := New("", "k.json")
	if err != nil {
		t.Fatalf("未配置应当不报错（静默 no-op）: %v", err)
	}
	if v.Enabled() {
		t.Fatal("未配置时不应启用")
	}
	if _, err := v.Encrypt([]byte("x"), "a"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("未启用时应返回 ErrDisabled，得到 %v", err)
	}
	if _, err := v.Decrypt([]byte("x"), "a"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("未启用时应返回 ErrDisabled，得到 %v", err)
	}
}

func TestGenerateKeysetParses(t *testing.T) {
	ks, err := GenerateKeysetJSON()
	if err != nil {
		t.Fatalf("GenerateKeysetJSON: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "k.json")
	if err := os.WriteFile(path, ks, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, "k.json"); err != nil {
		t.Fatalf("生成的 keyset 应当可被 New 解析: %v", err)
	}
}
