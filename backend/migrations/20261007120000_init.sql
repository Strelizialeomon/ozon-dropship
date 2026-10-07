-- S1 全部 13 张表（总纲 §6 关键字段与唯一键）。
-- 约定：主键 = 雪花 ID 字符串；金额 DECIMAL(18,4) + 币种列；时间按 UTC 存（DATETIME(3)）；
-- 业务表带 del_flag 软删标记；不建外键（引用一致性由应用层保证）。

-- +goose Up

-- +goose StatementBegin
CREATE TABLE stores (
    id                     VARCHAR(32)  NOT NULL,
    name                   VARCHAR(128) NOT NULL,
    mode                   VARCHAR(16)  NOT NULL COMMENT 'rfbs / fbp / local',
    client_id              VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'Ozon Client-Id',
    currency               CHAR(3)      NOT NULL DEFAULT 'CNY',
    default_relay_point_id VARCHAR(32)  NULL,
    push_enabled           TINYINT(1)   NOT NULL DEFAULT 0,
    ship_early             TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '备货提前（总纲 §13.1）',
    last_sync_at           DATETIME(3)  NULL COMMENT '最近一次同步成功（S1-D 写）',
    status                 VARCHAR(16)  NOT NULL DEFAULT 'active' COMMENT 'active / paused',
    created_at             DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at             DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag               TINYINT(1)   NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_stores_name (name),
    KEY idx_stores_status (status)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT 'Ozon 店铺';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE relay_points (
    id         VARCHAR(32)  NOT NULL,
    name       VARCHAR(128) NOT NULL,
    kind       VARCHAR(16)  NOT NULL COMMENT 'forwarder（货代/物流商代打包仓） / own_warehouse（自有仓）',
    address    VARCHAR(512) NOT NULL DEFAULT '',
    contact    VARCHAR(128) NOT NULL DEFAULT '',
    status     VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag   TINYINT(1)   NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_relay_points_name (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '中转点（总纲 §5.7）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE credentials (
    id               VARCHAR(32) NOT NULL,
    store_id         VARCHAR(32) NULL COMMENT '空 = 企业级凭据（1688 token 等，全平台共用）',
    kind             VARCHAR(32) NOT NULL COMMENT 'ozon_api_key / alibaba_app / alibaba_token',
    secret_enc       BLOB        NOT NULL COMMENT 'Tink 密文（AAD = credentials:行ID）',
    masked_tail      VARCHAR(8)  NOT NULL DEFAULT '' COMMENT '脱敏尾号（列表展示用，永不回明文）',
    expires_at       DATETIME(3) NULL,
    last_verified_at DATETIME(3) NULL,
    rotated_at       DATETIME(3) NULL,
    expiry_alert_stage INT NOT NULL DEFAULT 0 COMMENT '已告警档位（14/7/1），0 = 未告警；轮换后复位',
    created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag         TINYINT(1)  NOT NULL DEFAULT 0,
    -- store_id 为 NULL 的行在 MySQL 唯一索引里不去重，改用生成列把 NULL 归成空串，
    -- 保证「每种 kind 至多一行」对企业级凭据同样成立（总纲 §6）。
    store_key        VARCHAR(32) GENERATED ALWAYS AS (IFNULL(store_id, '')) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uk_credentials_scope (store_key, kind)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '凭据（总纲 §5.4/§5.8）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE supplier_offers (
    id                    VARCHAR(32)   NOT NULL,
    platform              VARCHAR(16)   NOT NULL COMMENT '1688 / pdd / taobao',
    item_id               VARCHAR(64)   NOT NULL,
    sku_id                VARCHAR(64)   NOT NULL DEFAULT '',
    url                   VARCHAR(512)  NOT NULL DEFAULT '',
    purchase_price        DECIMAL(18, 4) NOT NULL DEFAULT 0,
    domestic_freight      DECIMAL(18, 4) NOT NULL DEFAULT 0,
    currency              CHAR(3)       NOT NULL DEFAULT 'CNY',
    stock                 INT           NOT NULL DEFAULT 0,
    price_alert_threshold DECIMAL(6, 4) NULL COMMENT '较映射报价上涨阈值，比例（0.1000 = 10%）；空 = 默认 10%',
    order_channel         VARCHAR(16)   NOT NULL DEFAULT 'self_use' COMMENT 'self_use / cross_border / manual',
    followed              TINYINT(1)    NOT NULL DEFAULT 0 COMMENT '1688 商品是否已「关注」（库存推送前提）',
    status                VARCHAR(16)   NOT NULL DEFAULT 'active' COMMENT 'active / out_of_stock / invalid',
    created_at            DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at            DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag              TINYINT(1)    NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_supplier_offers_item (platform, item_id, sku_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '货源商品（总纲 §5.3）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE offer_links (
    id                VARCHAR(32) NOT NULL,
    store_id          VARCHAR(32) NOT NULL,
    ozon_offer_id     VARCHAR(128) NOT NULL,
    supplier_offer_id VARCHAR(32) NOT NULL,
    priority          INT         NOT NULL DEFAULT 1 COMMENT '数字小 = 主货源，大 = 备用',
    target_stock      INT         NOT NULL DEFAULT 0 COMMENT '恢复后写回库存（总纲 §5.10）',
    last_pushed_stock INT         NULL COMMENT '最近一次推送过的库存值',
    created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag          TINYINT(1)  NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_offer_links (store_id, ozon_offer_id, supplier_offer_id),
    KEY idx_offer_links_offer (store_id, ozon_offer_id, priority)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '按店货源映射（总纲 §5.3）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE orders (
    id                    VARCHAR(32)   NOT NULL,
    store_id              VARCHAR(32)   NOT NULL,
    posting_number        VARCHAR(64)   NOT NULL,
    order_number          VARCHAR(64)   NOT NULL DEFAULT '',
    parent_posting_number VARCHAR(64)   NULL COMMENT '拆单前的母 posting',
    status                VARCHAR(32)   NOT NULL DEFAULT 'new' COMMENT '内部状态机（总纲 §5.2）',
    ozon_status           VARCHAR(48)   NOT NULL DEFAULT '',
    ozon_substatus        VARCHAR(48)   NULL,
    tpl_integration_type  VARCHAR(32)   NULL COMMENT 'ozon / aggregator / 3pl_tracking / non_integrated / hybrid',
    ship_deadline         DATETIME(3)   NULL,
    relay_point_id        VARCHAR(32)   NULL,
    buyer_enc             BLOB          NULL COMMENT '买家信息密文（能少存就少存，总纲 §10 第 7 条）',
    total_amount          DECIMAL(18, 4) NOT NULL DEFAULT 0,
    currency              CHAR(3)       NOT NULL DEFAULT 'CNY',
    created_at            DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at            DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag              TINYINT(1)    NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_orders_posting (store_id, posting_number) COMMENT '幂等键（总纲 §5.5）',
    KEY idx_orders_status (store_id, status),
    KEY idx_orders_deadline (ship_deadline)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '订单（posting 粒度，总纲 §5.2）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE order_items (
    id            VARCHAR(32)   NOT NULL,
    order_id      VARCHAR(32)   NOT NULL,
    ozon_offer_id VARCHAR(128)  NOT NULL,
    qty           INT           NOT NULL DEFAULT 1,
    price         DECIMAL(18, 4) NOT NULL DEFAULT 0,
    currency      CHAR(3)       NOT NULL DEFAULT 'CNY',
    offer_link_id VARCHAR(32)   NULL,
    created_at    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag      TINYINT(1)    NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_order_items_order (order_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '订单行';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE purchase_tasks (
    id                VARCHAR(32)  NOT NULL,
    order_id          VARCHAR(32)  NOT NULL,
    supplier_offer_id VARCHAR(32)  NULL,
    channel           VARCHAR(16)  NOT NULL DEFAULT 'self_use' COMMENT 'self_use / cross_border / manual',
    executor_type     VARCHAR(16)  NOT NULL DEFAULT 'manual' COMMENT 'auto / manual',
    status            VARCHAR(16)  NOT NULL DEFAULT 'pending' COMMENT 'pending/executing/ordered/paid/shipped/closed/exception',
    payload           JSON         NULL COMMENT '下单参数快照',
    assignee          VARCHAR(64)  NULL,
    deadline          DATETIME(3)  NULL,
    idempotency_key   VARCHAR(128) NULL COMMENT '下单防重（总纲 §5.1）',
    created_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag          TINYINT(1)   NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_purchase_tasks_idem (idempotency_key),
    KEY idx_purchase_tasks_order (order_id),
    KEY idx_purchase_tasks_status (status, updated_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '采购任务（总纲 §5.1）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE purchase_orders (
    id                  VARCHAR(32)    NOT NULL,
    task_id             VARCHAR(32)    NOT NULL,
    platform_order_id   VARCHAR(64)    NOT NULL DEFAULT '',
    amount              DECIMAL(18, 4) NOT NULL DEFAULT 0 COMMENT '实付（价格快照）',
    currency            CHAR(3)        NOT NULL DEFAULT 'CNY',
    paid_at             DATETIME(3)    NULL,
    domestic_carrier    VARCHAR(64)    NULL COMMENT '国内段承运商（只在内部用，不回传 Ozon）',
    domestic_tracking_no VARCHAR(64)   NULL COMMENT '国内段快递号（只在内部用，不回传 Ozon）',
    created_at          DATETIME(3)    NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3)    NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag            TINYINT(1)     NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_purchase_orders_task (task_id),
    KEY idx_purchase_orders_platform (platform_order_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '采购单（总纲 §5.1/§7.4）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE shipments (
    id              VARCHAR(32) NOT NULL,
    order_id        VARCHAR(32) NOT NULL,
    tracking_no     VARCHAR(64) NULL,
    tracking_source VARCHAR(16) NULL COMMENT 'ozon（平台生成，我们只读） / seller（我们传）',
    carrier         VARCHAR(64) NULL,
    label_ref       VARCHAR(255) NULL COMMENT '面单引用（文件路径/ID）',
    handed_over_at  DATETIME(3) NULL,
    events          JSON        NULL COMMENT '轨迹事件',
    created_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag        TINYINT(1)  NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_shipments_order (order_id),
    KEY idx_shipments_tracking (tracking_no)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '发运（总纲 §7.4）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE exceptions (
    id        VARCHAR(32) NOT NULL,
    ref_type  VARCHAR(24) NOT NULL COMMENT 'order / purchase_task / shipment',
    ref_id    VARCHAR(32) NOT NULL,
    code      VARCHAR(64) NOT NULL COMMENT '异常类型码（总纲 §5.2 清单）',
    detail    TEXT        NULL,
    status    VARCHAR(16) NOT NULL DEFAULT 'open' COMMENT 'open / resolved',
    handled_by VARCHAR(64) NULL,
    handled_at DATETIME(3) NULL,
    note      TEXT        NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag  TINYINT(1)  NOT NULL DEFAULT 0,
    -- 「同一对象同一 code 未处理时去重」（总纲 §5.2）：把 open 行压成唯一键，
    -- resolved 行取 NULL 不受约束。业务侧插入撞 1062 即表示已存在未处理的同码异常。
    open_key  VARCHAR(128) GENERATED ALWAYS AS (IF(status = 'open', CONCAT(ref_type, '|', ref_id, '|', code), NULL)) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uk_exceptions_open (open_key),
    KEY idx_exceptions_status (status, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '异常池（总纲 §5.2）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE audit_logs (
    id     VARCHAR(32)  NOT NULL,
    actor  VARCHAR(128) NOT NULL DEFAULT '' COMMENT '用户，或 system',
    action VARCHAR(64)  NOT NULL,
    object VARCHAR(128) NOT NULL DEFAULT '',
    detail TEXT         NULL COMMENT 'JSON：前后差异等',
    at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_audit_logs_at (at),
    KEY idx_audit_logs_actor (actor),
    KEY idx_audit_logs_action (action)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '审计留痕（总纲 §5.11，只追加）';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE users (
    id            VARCHAR(32) NOT NULL,
    name          VARCHAR(64) NOT NULL,
    password_hash VARCHAR(100) NOT NULL,
    role          VARCHAR(16) NOT NULL DEFAULT 'operator' COMMENT 'admin / operator',
    status        VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT 'active / disabled',
    created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    del_flag      TINYINT(1)  NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_name (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci COMMENT '操作台用户（总纲 §6）';
-- +goose StatementEnd

-- +goose Down

DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS exceptions;
DROP TABLE IF EXISTS shipments;
DROP TABLE IF EXISTS purchase_orders;
DROP TABLE IF EXISTS purchase_tasks;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS offer_links;
DROP TABLE IF EXISTS supplier_offers;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS relay_points;
DROP TABLE IF EXISTS stores;
