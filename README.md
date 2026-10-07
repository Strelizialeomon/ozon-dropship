# ozon-dropship

Ozon（俄罗斯电商平台）店铺订单与中国货源平台（1688 等）的对接项目：订单同步、代发采购、发货物流、退货处理。

**当前状态**：需求已确认，总 spec 已定稿（v1.3）；S1 已拆 6 份子 spec（见总纲 §12.3），下一步按波次开实施 issue（未开工）。

## 从这里看起

- [总 spec：履约中台设计](docs/specs/spec-fulfillment-hub.md) —— 需求、架构、机制、数据模型、验收、波次。
- [决策记录（ADR）](docs/decisions/) —— 自研、货源接入、仓库结构、前后端技术栈、部署等长期决定。
- [Issue #1：前因后果 + 调研结论](https://github.com/Strelizialeomon/ozon-dropship/issues/1) —— 项目背景、Ozon / 中国货源 / 物流三侧调研详情、硬约束、待确认问题。
