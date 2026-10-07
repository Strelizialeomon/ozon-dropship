# testdata 说明

1688「买家自用版」权限批下来之前，按录制响应的替代品开发（子 spec §4 外部前置）。
本目录各文件的来源：

| 文件 | 来源 |
|---|---|
| `preview_response.json` | 官方文档「创建订单前预览数据接口」出参示例（open.1688.com 文档描述文件 2026-10-07 版） |
| `get_order_response.json` | 官方文档「订单详情查看(买家视角)」出参示例 |
| `list_orders_response.json` | 官方文档「订单列表查看(买家视角)」出参示例 |
| `logistics_infos_response.json` | 官方文档「获取交易订单的物流信息(买家视角)」返回示例 |
| `logistics_trace_response.json` | 官方文档「获取交易订单的物流跟踪信息(买家视角)」出参示例 |
| `create_order_response.json` | 按 `alibaba.trade.fastCreateOrder` 响应字段构造（字段与单位有三个独立来源互相印证：littlebossERP 生产代码按 `result.orderId` / `result.totalSuccessAmount ÷ 100` 取值；wzhsh90/go1688 与 bububa/go1688 的响应结构体同形） |
| `get_token_response.json` | 按 `system.oauth2.getToken` 响应字段构造（教程/社区口径，标【未验】，见 token.go 注释） |

官方样例原样保留（含示例里的测试账号数据），只做 JSON 校验，不改字段。

`signvec/` 目录是用官方 Java SDK 源码生成签名参考向量的工具与说明，见其中 README。
