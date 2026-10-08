-- M13 BOM 與工單(D64)
-- 數量一律是基本單位。BOM 單階:半成品有自己的 BOM 與工單,多階靠工單串接;BOM 之間不可循環。
CREATE TABLE boms (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT        NOT NULL REFERENCES companies (id),
    item_id     BIGINT        NOT NULL REFERENCES items (id),     -- 成品(商品類)
    yield_qty   NUMERIC(18,4) NOT NULL DEFAULT 1,                 -- 這份 BOM 的用量可產出的成品數量
    is_active   BOOLEAN       NOT NULL DEFAULT TRUE,
    note        TEXT          NOT NULL DEFAULT '',
    created_by  BIGINT        REFERENCES users (id),
    updated_by  BIGINT        REFERENCES users (id),
    version     INTEGER       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT boms_company_item_key UNIQUE (company_id, item_id),
    CONSTRAINT boms_yield_check CHECK (yield_qty > 0)
);
CREATE TRIGGER boms_set_updated_at BEFORE UPDATE ON boms FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE bom_lines (
    bom_id  BIGINT        NOT NULL REFERENCES boms (id) ON DELETE CASCADE,
    line_no INTEGER       NOT NULL,
    item_id BIGINT        NOT NULL REFERENCES items (id),         -- 材料
    qty     NUMERIC(18,4) NOT NULL,                               -- 每 yield_qty 成品的用量
    note    VARCHAR(255)  NOT NULL DEFAULT '',
    PRIMARY KEY (bom_id, line_no),
    CONSTRAINT bom_lines_qty_check CHECK (qty > 0)
);
CREATE INDEX bom_lines_item_idx ON bom_lines (item_id);

-- 工單:完工時一次領料並成品入庫(領料與入庫同一交易),狀態沿用共用狀態機,「過帳」即完工
CREATE TABLE work_orders (
    id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id            BIGINT        NOT NULL REFERENCES companies (id),
    doc_no                VARCHAR(30)   NOT NULL,
    doc_date              DATE          NOT NULL,
    item_id               BIGINT        NOT NULL REFERENCES items (id),   -- 成品
    plan_qty              NUMERIC(18,4) NOT NULL,                         -- 完工入庫數量
    warehouse_id          BIGINT        NOT NULL REFERENCES warehouses (id),  -- 成品入庫倉
    material_warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),  -- 領料倉
    processing_cost       NUMERIC(18,2) NOT NULL DEFAULT 0,               -- 加工費(人工 + 製造費用,本位幣),月結時計入成品成本
    output_lot_no         VARCHAR(40)   NOT NULL DEFAULT '',
    output_expiry         DATE,
    due_date              DATE,
    status                VARCHAR(20)   NOT NULL DEFAULT 'draft',
    note                  TEXT          NOT NULL DEFAULT '',
    submitted_by          BIGINT        REFERENCES users (id),
    submitted_at          TIMESTAMPTZ,
    approved_by           BIGINT        REFERENCES users (id),
    approved_at           TIMESTAMPTZ,
    posted_by             BIGINT        REFERENCES users (id),
    posted_at             TIMESTAMPTZ,
    created_by            BIGINT        REFERENCES users (id),
    updated_by            BIGINT        REFERENCES users (id),
    version               INTEGER       NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT work_orders_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT work_orders_status_check CHECK (status IN ('draft', 'pending', 'approved', 'posted', 'voided')),
    CONSTRAINT work_orders_qty_check CHECK (plan_qty > 0),
    CONSTRAINT work_orders_cost_check CHECK (processing_cost >= 0)
);
CREATE INDEX work_orders_company_date_idx ON work_orders (company_id, doc_date DESC);
CREATE INDEX work_orders_item_idx ON work_orders (item_id, status);
CREATE TRIGGER work_orders_set_updated_at BEFORE UPDATE ON work_orders FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 領料明細:開單時依 BOM 展開(可修改實際用量),過帳時照此領料
CREATE TABLE work_order_lines (
    work_order_id BIGINT        NOT NULL REFERENCES work_orders (id) ON DELETE CASCADE,
    line_no       INTEGER       NOT NULL,
    item_id       BIGINT        NOT NULL REFERENCES items (id),
    qty           NUMERIC(18,4) NOT NULL,
    lot_no        VARCHAR(40)   NOT NULL DEFAULT '',                  -- 批號管理的材料可指定批號,空白先到期先出
    note          VARCHAR(255)  NOT NULL DEFAULT '',
    PRIMARY KEY (work_order_id, line_no),
    CONSTRAINT work_order_lines_qty_check CHECK (qty > 0)
);
CREATE INDEX work_order_lines_item_idx ON work_order_lines (item_id);

-- 月結成本:材料領用(扣庫存,不計入銷貨成本)的數量與金額
ALTER TABLE item_costs ADD COLUMN consume_qty   NUMERIC(18,4) NOT NULL DEFAULT 0;
ALTER TABLE item_costs ADD COLUMN consume_value NUMERIC(18,4) NOT NULL DEFAULT 0;

INSERT INTO doc_number_rules (company_id, doc_type, name, prefix)
SELECT c.id, 'work_order', '工單', 'WO' FROM companies c ON CONFLICT DO NOTHING;

-- 加工費轉出(貸方的成本科目,對沖實際發生的人工與製造費用)與拋轉規則
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '5103', '加工費轉出', 'cost' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'cost.absorb', a.id FROM accounts a WHERE a.code = '5103'
ON CONFLICT (company_id, map_key) DO NOTHING;
