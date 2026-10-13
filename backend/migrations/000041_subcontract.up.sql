-- 委外加工(D100):發料給加工廠(轉入該廠的委外倉)→ 收回成品(扣委外倉材料、成品入庫、加工費轉應付)
ALTER TABLE warehouses
    ADD COLUMN kind        VARCHAR(12) NOT NULL DEFAULT 'normal',          -- normal / subcontract(委外倉)
    ADD COLUMN supplier_id BIGINT      REFERENCES suppliers (id),          -- 委外倉對應的加工廠
    ADD CONSTRAINT warehouses_kind_check CHECK (kind IN ('normal', 'subcontract')),
    ADD CONSTRAINT warehouses_kind_supplier_check CHECK ((kind = 'subcontract') = (supplier_id IS NOT NULL));
CREATE UNIQUE INDEX warehouses_subcontract_supplier_key ON warehouses (company_id, supplier_id) WHERE kind = 'subcontract';

CREATE TABLE subcontract_orders (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id           BIGINT        NOT NULL REFERENCES companies (id),
    doc_no               VARCHAR(30)   NOT NULL,
    doc_date             DATE          NOT NULL,
    supplier_id          BIGINT        NOT NULL REFERENCES suppliers (id),   -- 加工廠
    item_id              BIGINT        NOT NULL REFERENCES items (id),       -- 收回的成品
    plan_qty             NUMERIC(18,4) NOT NULL,
    unit_fee             NUMERIC(18,4) NOT NULL DEFAULT 0,                   -- 預計加工費單價(本位幣,含稅)
    issue_warehouse_id   BIGINT        NOT NULL REFERENCES warehouses (id),  -- 發料倉
    receive_warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),  -- 成品收回倉
    sc_warehouse_id      BIGINT        NOT NULL REFERENCES warehouses (id),  -- 加工廠的委外倉
    due_date             DATE,
    status               VARCHAR(10)   NOT NULL DEFAULT 'open',              -- open / closed / voided
    note                 TEXT          NOT NULL DEFAULT '',
    created_by           BIGINT        REFERENCES users (id),
    updated_by           BIGINT        REFERENCES users (id),
    version              INTEGER       NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT subcontract_orders_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT subcontract_orders_status_check CHECK (status IN ('open', 'closed', 'voided')),
    CONSTRAINT subcontract_orders_qty_check CHECK (plan_qty > 0),
    CONSTRAINT subcontract_orders_fee_check CHECK (unit_fee >= 0)
);
CREATE INDEX subcontract_orders_company_date_idx ON subcontract_orders (company_id, doc_date DESC);
CREATE INDEX subcontract_orders_supplier_idx ON subcontract_orders (supplier_id, status);
CREATE TRIGGER subcontract_orders_set_updated_at BEFORE UPDATE ON subcontract_orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 預計材料用量(整張委外單的總量)
CREATE TABLE subcontract_order_lines (
    order_id BIGINT        NOT NULL REFERENCES subcontract_orders (id) ON DELETE CASCADE,
    line_no  INTEGER       NOT NULL,
    item_id  BIGINT        NOT NULL REFERENCES items (id),
    qty      NUMERIC(18,4) NOT NULL,
    note     VARCHAR(255)  NOT NULL DEFAULT '',
    PRIMARY KEY (order_id, line_no),
    CONSTRAINT subcontract_order_lines_qty_check CHECK (qty > 0)
);

-- 發料 / 收回 / 退料,過帳即生效
CREATE TABLE subcontract_txns (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    order_id        BIGINT        NOT NULL REFERENCES subcontract_orders (id),
    kind            VARCHAR(10)   NOT NULL,                       -- issue 發料 / receive 收回 / return 退料
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    qty             NUMERIC(18,4) NOT NULL DEFAULT 0,             -- 收回:成品數量;其他為 0
    processing_fee  NUMERIC(18,2) NOT NULL DEFAULT 0,             -- 收回:本次加工費(本位幣,含稅),同時是應付金額
    output_lot_no   VARCHAR(40)   NOT NULL DEFAULT '',
    output_expiry   DATE,
    output_bin_code VARCHAR(20)   NOT NULL DEFAULT '',
    note            VARCHAR(255)  NOT NULL DEFAULT '',
    status          VARCHAR(10)   NOT NULL DEFAULT 'posted',      -- posted / voided
    created_by      BIGINT        REFERENCES users (id),
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    voided_by       BIGINT        REFERENCES users (id),
    voided_at       TIMESTAMPTZ,
    CONSTRAINT subcontract_txns_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT subcontract_txns_kind_check CHECK (kind IN ('issue', 'receive', 'return')),
    CONSTRAINT subcontract_txns_status_check CHECK (status IN ('posted', 'voided')),
    CONSTRAINT subcontract_txns_qty_check CHECK (qty >= 0 AND (kind = 'receive') = (qty > 0)),
    CONSTRAINT subcontract_txns_fee_check CHECK (processing_fee >= 0)
);
CREATE INDEX subcontract_txns_order_idx ON subcontract_txns (order_id);
CREATE INDEX subcontract_txns_date_idx ON subcontract_txns (company_id, doc_date) WHERE status = 'posted';

CREATE TABLE subcontract_txn_lines (
    txn_id   BIGINT        NOT NULL REFERENCES subcontract_txns (id) ON DELETE CASCADE,
    line_no  INTEGER       NOT NULL,
    item_id  BIGINT        NOT NULL REFERENCES items (id),
    qty      NUMERIC(18,4) NOT NULL,
    lot_no   VARCHAR(40)   NOT NULL DEFAULT '',
    bin_code VARCHAR(20)   NOT NULL DEFAULT '',
    PRIMARY KEY (txn_id, line_no),
    CONSTRAINT subcontract_txn_lines_qty_check CHECK (qty > 0)
);

INSERT INTO doc_number_rules (company_id, doc_type, name, prefix)
SELECT c.id, v.t, v.n, v.p FROM companies c,
    (VALUES ('subcontract_order', '委外加工單', 'SC'), ('subcontract_issue', '委外發料', 'SI'),
            ('subcontract_receive', '委外收回', 'SR'), ('subcontract_return', '委外退料', 'SB')) AS v (t, n, p)
ON CONFLICT DO NOTHING;
