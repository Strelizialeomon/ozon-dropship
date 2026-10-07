// Package vault 是凭据保险箱（总纲 §5.4）：
// 用 Tink 的 AEAD 加密所有密钥，密文绑定所在表与行（关联数据 = 表名 + 行 ID），
// 挪到别的行解不开；主密钥由 systemd LoadCredentialEncrypted 注入，不进 git、不放环境变量。
//
// 未配置（credentials_dir 为空）时保险箱不启用：服务照常启动（标准档静默 no-op），
// 但读写一律返回 ErrDisabled——绝不落明文是硬底线。
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
)

// ErrDisabled 保险箱未启用（没配 credentials_dir，或主密钥文件缺失）。
var ErrDisabled = errors.New("凭据保险箱未启用：缺少主密钥配置")

// Vault 加解密器。
type Vault struct {
	aead tink.AEAD
}

// New 从 systemd 凭据目录读主密钥（Tink keyset JSON）并构造 AEAD。
// credentialsDir 为空 → 返回未启用的空保险箱（不报错）。
//
// 密钥文件本身是 systemd 解密后注入的运行时文件（LoadCredentialEncrypted），
// 磁盘上存的是密文、权限只给服务进程，所以这里用 insecurecleartextkeyset 读取
// 明文 keyset 是正确的用法——"不加密"的是这份已被 systemd 保护的文件，
// 不是数据。（参考：systemd credentials 机制。）
func New(credentialsDir, masterKeyFile string) (*Vault, error) {
	if credentialsDir == "" {
		return &Vault{}, nil
	}
	path := filepath.Join(credentialsDir, masterKeyFile)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开主密钥失败(%s): %w", path, err)
	}
	defer func() { _ = f.Close() }()

	handle, err := insecurecleartextkeyset.Read(keyset.NewJSONReader(f))
	if err != nil {
		return nil, fmt.Errorf("解析主密钥 keyset 失败: %w", err)
	}
	a, err := aead.New(handle)
	if err != nil {
		return nil, fmt.Errorf("构造 AEAD 失败: %w", err)
	}
	return &Vault{aead: a}, nil
}

// Enabled 是否可用。
func (v *Vault) Enabled() bool { return v != nil && v.aead != nil }

// AADFor 组装关联数据：表名 + 行 ID。加密与解密两端必须传同一个值，
// 换行注入的密文因 AAD 不匹配直接解密失败（总纲 §5.4「挪到别的行解不开」）。
func AADFor(table, rowID string) string { return table + ":" + rowID }

// Encrypt 加密。
func (v *Vault) Encrypt(plaintext []byte, aad string) ([]byte, error) {
	if !v.Enabled() {
		return nil, ErrDisabled
	}
	return v.aead.Encrypt(plaintext, []byte(aad))
}

// Decrypt 解密。
func (v *Vault) Decrypt(ciphertext []byte, aad string) ([]byte, error) {
	if !v.Enabled() {
		return nil, ErrDisabled
	}
	return v.aead.Decrypt(ciphertext, []byte(aad))
}

// GenerateKeysetJSON 生成一份新的 AES256-GCM keyset JSON（部署时造主密钥用，
// 见 cmd/genvaultkey）。运维流程：生成 → 用 systemd-creds 加密 → LoadCredentialEncrypted。
func GenerateKeysetJSON() ([]byte, error) {
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		return nil, fmt.Errorf("生成 keyset 失败: %w", err)
	}
	var buf bytesBuffer
	if err := insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(&buf)); err != nil {
		return nil, fmt.Errorf("写出 keyset 失败: %w", err)
	}
	return buf.Bytes(), nil
}

// bytesBuffer 最小化依赖：避免为写出 keyset 引 bufio。
type bytesBuffer struct{ b []byte }

func (w *bytesBuffer) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *bytesBuffer) Bytes() []byte               { return w.b }
