-- 在製品(WIP,D101):工單完工前可分批領料,材料轉入公司唯一的在製品倉,完工回報時優先從在製品扣
ALTER TABLE warehouses DROP CONSTRAINT warehouses_kind_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_kind_check CHECK (kind IN ('normal', 'subcontract', 'wip'));
CREATE UNIQUE INDEX warehouses_wip_key ON warehouses (company_id) WHERE kind = 'wip';

CREATE TABLE work_order_wip_txns (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id    BIGINT       NOT NULL REFERENCES companies (id),
    work_order_id BIGINT       NOT NULL REFERENCES work_orders (id),
    kind          VARCHAR(10)  NOT NULL,                       -- issue 領料 / return 退料
    doc_no        VARCHAR(30)  NOT NULL,
    doc_date      DATE         NOT NULL,
    note          VARCHAR(255) NOT NULL DEFAULT '',
    status        VARCHAR(10)  NOT NULL DEFAULT 'posted',      -- posted / voided
    created_by    BIGINT       REFERENCES users (id),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    voided_by     BIGINT       REFERENCES users (id),
    voided_at     TIMESTAMPTZ,
    CONSTRAINT work_order_wip_txns_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT work_order_wip_txns_kind_check CHECK (kind IN ('issue', 'return')),
    CONSTRAINT work_order_wip_txns_status_check CHECK (status IN ('posted', 'voided'))
);
CREATE INDEX work_order_wip_txns_wo_idx ON work_order_wip_txns (work_order_id);

CREATE TABLE work_order_wip_txn_lines (
    txn_id   BIGINT        NOT NULL REFERENCES work_order_wip_txns (id) ON DELETE CASCADE,
    line_no  INTEGER       NOT NULL,
    item_id  BIGINT        NOT NULL REFERENCES items (id),
    qty      NUMERIC(18,4) NOT NULL,
    lot_no   VARCHAR(40)   NOT NULL DEFAULT '',
    bin_code VARCHAR(20)   NOT NULL DEFAULT '',                -- 領料:來源儲位;退料:回料倉的入庫儲位
    PRIMARY KEY (txn_id, line_no),
    CONSTRAINT work_order_wip_txn_lines_qty_check CHECK (qty > 0)
);

-- 完工回報的領料中有多少是從在製品倉扣的(其餘從領料倉)
ALTER TABLE work_order_completion_lines ADD COLUMN from_wip NUMERIC(18,4) NOT NULL DEFAULT 0;

INSERT INTO doc_number_rules (company_id, doc_type, name, prefix)
SELECT c.id, v.t, v.n, v.p FROM companies c,
    (VALUES ('work_order_wip_issue', '工單領料', 'WI'), ('work_order_wip_return', '工單退料', 'WT')) AS v (t, n, p)
ON CONFLICT DO NOTHING;
