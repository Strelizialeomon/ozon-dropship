# testdata：接口响应样例

单测用的响应样例（httptest 直接回放，不连真实店）。

**来源**：除 `listdeliverymethods_response.json` 外，全部取自 Ozon 官方 Seller API
文档的示例（docs.ozon.ru swagger，2026-10-07 核对，快照 sha 记于实施 PR）。
`listdeliverymethods_response.json` 官方未给示例，照官方 schema 手造，字段结构
与 schema 一致。

**与「真实店录制」的差距**：这些是官方示例而非真实店录制——字段结构权威，
但个别值（时间、单号）是文档示例值。真实店冒烟（`smoke` 构建标签）跑通后，
可逐步替换为真实录制（保持文件名不变，单测无需改动）。

**每个文件对应的接口**：

| 文件 | 接口 |
|---|---|
| `listpostings_response.json` | POST /v4/posting/fbs/list |
| `listunfulfilled_response.json` | POST /v4/posting/fbs/unfulfilled/list |
| `getposting_response.json` | POST /v3/posting/fbs/get |
| `shipposting_response.json` | POST /v4/posting/fbs/ship |
| `getroles_response.json` | POST /v1/roles |
| `settrackingnumber_response.json` | POST /v2/fbs/posting/tracking-number/set |
| `listdeliverymethods_response.json` | POST /v2/delivery-method/list（手造） |
