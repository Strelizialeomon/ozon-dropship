-- S1-D 重审修订：三张表补唯一键，堵住并发写重复行的数据根因。
--
-- 背景（PR #16 重审路二 #2 / #7 / #8）：
--   - order_items：同一订单同一 ozon_offer_id 重复行 → 采购任务把数量翻倍下单；
--   - purchase_orders：同一任务两行采购单（先查后插的并发窗口）；
--   - shipments：同一订单两行发运记录（签收/取面单并发）。
-- 迁移前先按「保留最早一行」去重，避免历史脏数据挡住建键。

-- +goose Up

-- +goose StatementBegin
DELETE t1 FROM order_items t1
JOIN order_items t2
  ON t1.order_id = t2.order_id
 AND t1.ozon_offer_id = t2.ozon_offer_id
 AND (t1.created_at > t2.created_at OR (t1.created_at = t2.created_at AND t1.id > t2.id));
-- +goose StatementEnd

-- +goose StatementBegin
DELETE p1 FROM purchase_orders p1
JOIN purchase_orders p2
  ON p1.task_id = p2.task_id
 AND (p1.created_at > p2.created_at OR (p1.created_at = p2.created_at AND p1.id > p2.id));
-- +goose StatementEnd

-- +goose StatementBegin
DELETE s1 FROM shipments s1
JOIN shipments s2
  ON s1.order_id = s2.order_id
 AND (s1.created_at > s2.created_at OR (s1.created_at = s2.created_at AND s1.id > s2.id));
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE order_items
    ADD UNIQUE KEY uk_order_items_order_offer (order_id, ozon_offer_id) COMMENT '一单一商品一行（upsert 幂等键）';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE purchase_orders
    ADD UNIQUE KEY uk_purchase_orders_task (task_id) COMMENT '一个采购任务一张采购单';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE shipments
    ADD UNIQUE KEY uk_shipments_order (order_id) COMMENT '一个订单一条发运记录';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE shipments DROP INDEX uk_shipments_order;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE purchase_orders DROP INDEX uk_purchase_orders_task;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE order_items DROP INDEX uk_order_items_order_offer;
-- +goose StatementEnd
