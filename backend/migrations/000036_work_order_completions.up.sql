-- 工單分批完工(D94):每次完工回報一筆,回報即過帳(領料 + 成品入庫同一交易)
CREATE TABLE work_order_completions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    work_order_id   BIGINT        NOT NULL REFERENCES work_orders (id),
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    qty             NUMERIC(18,4) NOT NULL,                       -- 本次完工入庫數量
    processing_cost NUMERIC(18,2) NOT NULL DEFAULT 0,             -- 本次加工費(本位幣),月結計入成品成本
    output_lot_no   VARCHAR(40)   NOT NULL DEFAULT '',
    output_expiry   DATE,
    output_bin_code VARCHAR(20)   NOT NULL DEFAULT '',
    note            VARCHAR(255)  NOT NULL DEFAULT '',
    status          VARCHAR(10)   NOT NULL DEFAULT 'posted',      -- posted / voided
    created_by      BIGINT        REFERENCES users (id),
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    voided_by       BIGINT        REFERENCES users (id),
    voided_at       TIMESTAMPTZ,
    CONSTRAINT work_order_completions_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT work_order_completions_qty_check CHECK (qty > 0),
    CONSTRAINT work_order_completions_cost_check CHECK (processing_cost >= 0),
    CONSTRAINT work_order_completions_status_check CHECK (status IN ('posted', 'voided'))
);
CREATE INDEX work_order_completions_wo_idx ON work_order_completions (work_order_id);
CREATE INDEX work_order_completions_date_idx ON work_order_completions (company_id, doc_date) WHERE status = 'posted';

CREATE TABLE work_order_completion_lines (
    completion_id BIGINT        NOT NULL REFERENCES work_order_completions (id) ON DELETE CASCADE,
    line_no       INTEGER       NOT NULL,
    item_id       BIGINT        NOT NULL REFERENCES items (id),
    qty           NUMERIC(18,4) NOT NULL,
    lot_no        VARCHAR(40)   NOT NULL DEFAULT '',
    bin_code      VARCHAR(20)   NOT NULL DEFAULT '',
    PRIMARY KEY (completion_id, line_no),
    CONSTRAINT work_order_completion_lines_qty_check CHECK (qty > 0)
);

INSERT INTO doc_number_rules (company_id, doc_type, name, prefix)
SELECT c.id, 'work_order_completion', '完工回報', 'WC' FROM companies c ON CONFLICT DO NOTHING;
