# 签名参考向量生成器（验收第 1 条）

子 spec 验收：「同一组参数，签名结果与官方 Java SDK 一致」。这里放生成对照值的工具：

- `src/com/alibaba/tuna/util/SignatureUtil.java`——官方 SDK 源码原样拷贝，来自
  <https://github.com/alibaba/AliOpen>（`tuna-java-sdk`，Apache-2.0，文件头保留原版权声明）。
  它的 `hmacSha1(path, params, signingKey)` 就是签名算法本体：先写签名路径，再写按
  key+value 拼接串排序后的全部参数，HMAC-SHA1，`encodeHexStr` 输出大写十六进制。
- `src/SignVec.java`——固定参数集（与 `sign_test.go` 里的向量逐项一致），调上述官方
  函数打印签名值。

## 重新生成

需要 JDK（任意现代版本）：

```bash
cd testdata/signvec
javac -encoding UTF-8 -d classes $(find src -name "*.java")
java -cp classes -Dfile.encoding=UTF-8 SignVec
```

输出即 `sign_test.go` 中 `TestSignOfficialVectors` 的参考值（参数、路径、密钥与断言逐字对应）。
注意：官方调用方与校验端都会先把 `_aop_signature` 从参数里剔掉再签名，`SignVec` 照此语义
（向量 V4 专门覆盖「带 _aop_signature 时它不参与签名」）。

## 说明

- 2026-10-07 生成时本机用 Homebrew openjdk@17 编译运行，输出与仓库内断言一致。
- 这是与「官方 Java SDK 得同一结果」的最直接证据；与**真实网关**的一致性仍要等权限批下来
  实测（子 spec 已标【未验】）。
