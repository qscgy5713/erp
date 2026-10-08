-- M7 月結成本:月加權平均成本、銷貨成本傳票(D51)

-- 月結紀錄:每月至多一筆;存在即表示該月庫存異動已鎖定
CREATE TABLE cost_closings (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id        BIGINT        NOT NULL REFERENCES companies (id),
    period            CHAR(7)       NOT NULL,                  -- YYYY-MM
    item_count        INTEGER       NOT NULL DEFAULT 0,
    cogs_amount       NUMERIC(18,4) NOT NULL DEFAULT 0,        -- 銷貨成本(已拋傳票)
    adjust_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,        -- 盤點 / 調整的存貨損益淨額(正為損失)
    inventory_value   NUMERIC(18,4) NOT NULL DEFAULT 0,        -- 期末存貨金額(各料品期末數量 × 平均成本)
    closed_by         BIGINT        REFERENCES users (id),
    closed_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT cost_closings_company_period_key UNIQUE (company_id, period),
    CONSTRAINT cost_closings_period_format CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$')
);

-- 各料品當月成本計算結果(同時是下月的期初)
CREATE TABLE item_costs (
    closing_id     BIGINT        NOT NULL REFERENCES cost_closings (id) ON DELETE CASCADE,
    item_id        BIGINT        NOT NULL REFERENCES items (id),
    opening_qty    NUMERIC(18,4) NOT NULL,
    opening_value  NUMERIC(18,4) NOT NULL,
    purchase_qty   NUMERIC(18,4) NOT NULL,                     -- 進貨 − 進貨退出
    purchase_value NUMERIC(18,4) NOT NULL,
    sales_qty      NUMERIC(18,4) NOT NULL,                     -- 出貨(負)+ 銷貨退回(正)
    adjust_qty     NUMERIC(18,4) NOT NULL,                     -- 盤點 / 調整 / 其他
    avg_cost       NUMERIC(18,6) NOT NULL,                     -- 當月平均單位成本(基本單位)
    cogs_amount    NUMERIC(18,4) NOT NULL,
    adjust_amount  NUMERIC(18,4) NOT NULL,
    closing_qty    NUMERIC(18,4) NOT NULL,
    closing_value  NUMERIC(18,4) NOT NULL,
    PRIMARY KEY (closing_id, item_id)
);
CREATE INDEX item_costs_item_idx ON item_costs (item_id);

-- 存貨盤損益科目與拋轉規則
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '5102', '存貨盤損(盈)', 'cost' FROM companies c
ON CONFLICT (company_id, code) DO NOTHING;

INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, m.map_key, a.id
FROM accounts a
JOIN (VALUES
    ('cost.cogs', '5101'),
    ('cost.inventory', '1141'),
    ('cost.adjustment', '5102')
) AS m (map_key, code) ON m.code = a.code
ON CONFLICT (company_id, map_key) DO NOTHING;
