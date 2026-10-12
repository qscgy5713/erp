-- 售價表:客戶別 / 一般售價 + 數量階梯 + 有效期間(單價為未稅、基本單位)
CREATE TABLE sales_prices (
    id          BIGSERIAL     PRIMARY KEY,
    company_id  BIGINT        NOT NULL REFERENCES companies (id),
    customer_id BIGINT        REFERENCES customers (id), -- NULL = 所有客戶
    item_id     BIGINT        NOT NULL REFERENCES items (id),
    currency    CHAR(3)       NOT NULL,
    min_qty     NUMERIC(18,4) NOT NULL DEFAULT 0,        -- 基本單位數量 ≥ 此值才適用
    unit_price  NUMERIC(18,6) NOT NULL,
    valid_from  DATE,
    valid_to    DATE,
    note        TEXT          NOT NULL DEFAULT '',
    created_by  BIGINT        REFERENCES users (id),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT sales_prices_check CHECK (min_qty >= 0 AND unit_price >= 0 AND (valid_to IS NULL OR valid_from IS NULL OR valid_to >= valid_from))
);
CREATE INDEX sales_prices_item_idx ON sales_prices (company_id, item_id);
