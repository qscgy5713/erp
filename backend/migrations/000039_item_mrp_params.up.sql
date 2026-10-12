-- MRP 計畫參數(D98):前置時間、最小訂購 / 生產量、批量倍數;沒有資料列 = 全部為 0(不限制)
CREATE TABLE item_mrp_params (
    item_id         BIGINT        PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    lead_time_days  INTEGER       NOT NULL DEFAULT 0,       -- 採購件 = 採購前置,有 BOM 的料品 = 生產前置
    min_order_qty   NUMERIC(18,4) NOT NULL DEFAULT 0,       -- 基本單位;0 = 不限
    order_multiple  NUMERIC(18,4) NOT NULL DEFAULT 0,       -- 批量倍數(基本單位);0 = 不限
    updated_by      BIGINT        REFERENCES users (id),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT item_mrp_params_lead_check CHECK (lead_time_days BETWEEN 0 AND 365),
    CONSTRAINT item_mrp_params_min_check CHECK (min_order_qty >= 0),
    CONSTRAINT item_mrp_params_multiple_check CHECK (order_multiple >= 0)
);
CREATE INDEX item_mrp_params_company_idx ON item_mrp_params (company_id);
