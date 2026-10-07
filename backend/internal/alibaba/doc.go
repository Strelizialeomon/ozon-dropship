// Package alibaba 是 1688 开放平台买家侧客户端（子 spec S1-C，总纲 §7.2）。
//
// 范围（S1）：
//   - 「采购解决方案（买家自用版）」通道一：alibaba.trade.fastCreateOrder 下单；
//   - 下单预览、订单详情、买家订单列表、买家视角物流（含轨迹）；
//   - HMAC-SHA1 签名（自写，不用第三方 Go 库）、token 自动续期（总纲 §5.8）；
//   - 请求统一经 S1-A 的限流器（ratelimit.ScopeAlibaba 企业级桶，总纲 §5.5）。
//
// 不做（归属别的份或波次）：何时下单/防重（S1-D）、跨境自用版 createCrossOrder、免密支付、
// 推送（S2）。
//
// 依赖方向：只依赖 internal/infra（ratelimit），不 import 业务包（ADR-20261007-go-package-deps）。
// 本包在需要凭据时定义一个小的 CredentialStore 接口（接口放使用方），装配层用
// store.CredentialService 适配后接进 NewClient——本包不 import store。
//
// # 协议依据（2026-10-07 调研）
//
// 签名算法：参数按名称 ASCII 升序，拼接「参数名+参数值」原文（除 _aop_signature 自身），
// 前缀签名路径 param2/{version}/{namespace}/{name}/{appKey}，用 appSecret 做 HMAC-SHA1，
// 输出大写十六进制。三个独立实现互相印证：
//   - 阿里官方 org 的 SDK 源码：https://github.com/alibaba/AliOpen（tuna-java-sdk 的
//     com.alibaba.tuna.util.SignatureUtil，Apache-2.0）；
//   - 官方老版 open-sdk 的 CommonUtil（buildInvokeUrlPath + signatureWithParamsAndUrlPath）；
//   - 现代可用实现（EalenXie/sdk-all、wzhsh90/go1688），算法一致。
//
// 参考向量由 testdata/signvec 用官方 SDK 源码真跑生成，见 sign_test.go。
//
// 接口参数与响应结构：取自 1688 官方 API 文档（open.1688.com/api/apidocdetail.htm?id=...）的
// 文档描述文件与出参示例；本仓库 testdata/ 保存了整理后的响应样例。
//
// # 明确标【未验】的部分（等 1688「买家自用版」权限批下来后实测）
//
//   - 签名与真实网关的一致性（本地只与官方 SDK 参考向量比对过，没对联调过真实网关）；
//   - system.oauth2.getToken 的端点、参数、响应字段与「无需签名」的说法（二手教程口径）；
//   - 超限错误的真实形态（官方未公开限额；本包按 HTTP 429 与一组候选错误码识别，见
//     errors.go 的 rateLimitCodes）；
//   - 金额单位口径（预览/下单响应按【分】，订单详情按【元】，以官方文档字段说明为准，
//     字段注释里逐条标明）。
//
// 冒烟（真实账号、会花真钱）见 smoke_test.go，带构建标签 smoke，默认不跑。
package alibaba
