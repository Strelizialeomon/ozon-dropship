# spec-s1c-alibaba-client —— S1-C 1688 客户端（子 spec）

> **总 spec = 设计权威**：[spec-fulfillment-hub](spec-fulfillment-hub.md)（下称「总纲」），本文是其子件 **S1-C**，只写落地；与总纲或生效 ADR 冲突时以它们为准。
> Issue: 待开 ｜ 状态：v1.0（2026-10-07）｜ 发布序：**第 2 批**，S1-A 合并后开工；与 B、D、F 并行
> 跨份验收与协作声明：S1 父 issue（待开）

## 1. 管什么 / 不管什么

| 管 | 不管 |
|---|---|
| `internal/alibaba`：买家自用版（通道一）S1 用到的调用 | 何时下单、下单防重流程、人工渠道（S1-D） |
| 签名、授权与 token 续期、错误归一、接限流器 | 跨境自用版、免密支付、消息推送、商品关注（S2） |

## 2. 地盘

- **独占**：`backend/internal/alibaba/**`。
- **不碰**：其余全部。
- **依赖方向**：只依赖 `internal/infra`，不 import 业务包。token 要存进 `credentials` 表：本包定义一个小的「token 存取」接口，方法签名与 S1-A `store` 包对外的凭据读写方法对齐（见 S1-A §3 第 5 条），装配层把 `store` 接进来——**不改 `store` 的代码**（[ADR-20261007-go-package-deps](../decisions/2026-10-07-go-package-deps.md)）。

## 3. 对外方法清单（S1-D 据此在使用方定义小接口）

| 方法 | 用途 |
|---|---|
| `PreviewOrder` | 下单预览（运费、能否下单） |
| `CreateOrder` | 用 `alibaba.trade.fastCreateOrder` 下单：收货地址 = 中转点，买家留言带 `posting_number` |
| `GetOrder` | 订单详情：状态、实付金额 |
| `ListBuyerOrders` | 按时间窗查买家订单（下单防重核对用） |
| `GetLogistics` | 买家视角物流信息：国内段单号与轨迹 |

具体接口名以「采购解决方案（买家自用版）」在 1688 开放平台列出的为准（总纲 §7.2）。

## 4. 要做的事

1. **签名**：HMAC-SHA1，规则照 1688 官方签名文档；自写，不用第三方 Go 库。
2. **授权与 token**：`access_token` 10 小时有效，到期前用 `refresh_token` 续；续失败返回可识别的错误，供到期告警用（总纲 §5.8）。
3. 实现上表 5 个方法，请求经 S1-A 的限流器。
4. **测试**：签名用官方 Java SDK 对同一组参数生成参考值对照；各方法 httptest + 录制响应。
5. **冒烟**（人工执行，会花真钱）：对一个老商家下一单低价商品，人工在 1688 付款，再查订单和物流。

**外部前置**：1688「买家自用版」权限已批（总纲 §12.1）。没批也能按录制响应开发；冒烟等批下来再做。

## 5. 验收

- [ ] 同一组参数，签名结果与官方 Java SDK 一致
- [ ] token 快到期自动续；续失败返回可识别的错误
- [ ] 方法清单每个方法都有录制响应单测
- [ ] 冒烟：真实账号对老商家下一单、人工付款后，`GetOrder`、`GetLogistics` 能查到

## 6. 机制清单

无新增机制。落地总纲 §5.5 限流重试（1688 侧）与 §5.8 凭据到期告警（1688 token 部分）。

## 7. 自定细节

- 方法命名如第 3 节表；token 存 `credentials` 表，`kind = alibaba_token`，经 S1-A 的保险箱加密；1688 token 属于企业账号、不属于某个店，`store_id` 留空。
- 录制响应放 `internal/alibaba/testdata/`；冒烟测试构建标签 `smoke`。

## 8. 审核修订记录

（审核完成后追加：# / 严重度 / 发现 / 处置）
