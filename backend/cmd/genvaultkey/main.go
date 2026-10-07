// genvaultkey 生成凭据保险箱的主密钥（Tink AES256-GCM keyset JSON）。
//
// 运维流程（部署时做一次）：
//
//	go run ./cmd/genvaultkey -out vault_keyset.json
//	systemd-creds encrypt vault_keyset.json /etc/credstore.encrypted/vault_keyset.json
//	# unit 里 LoadCredentialEncrypted=vault_keyset.json:/etc/credstore.encrypted/vault_keyset.json
//
// 明文 keyset 只应存在于 systemd 加密后的凭据库里；生成后删掉本地明文文件。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Strelizialeomon/ozon-dropship/backend/internal/infra/vault"
)

func main() {
	out := flag.String("out", "vault_keyset.json", "输出文件路径")
	flag.Parse()

	ks, err := vault.GenerateKeysetJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成失败: %v\n", err)
		os.Exit(1)
	}
	// 0600：主密钥文件只有属主可读。
	if err := os.WriteFile(*out, ks, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "写文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成 %s（%d 字节）\n", *out, len(ks))
	fmt.Println("下一步：systemd-creds encrypt 后用 LoadCredentialEncrypted 注入，装完删掉本地明文。")
}
