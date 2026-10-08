-- M2 庫存核心:流水帳、現有量、庫存單據(調整 / 調撥 / 盤點)

-- 庫存單據:調整、調撥、盤點共用(D25)
CREATE TABLE stock_documents (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT       NOT NULL REFERENCES companies (id),
    -- adjustment 調整 / transfer 調撥 / count 盤點
    doc_type        VARCHAR(20)  NOT NULL,
    doc_no          VARCHAR(30)  NOT NULL,
    doc_date        DATE         NOT NULL,
    warehouse_id    BIGINT       NOT NULL REFERENCES warehouses (id),   -- 調撥時為「調出倉」
    to_warehouse_id BIGINT       REFERENCES warehouses (id),            -- 僅調撥
    category_id     BIGINT       REFERENCES item_categories (id),       -- 僅盤點:盤點範圍
    status          VARCHAR(20)  NOT NULL DEFAULT 'draft',
    note            TEXT         NOT NULL DEFAULT '',
    submitted_by    BIGINT       REFERENCES users (id),
    submitted_at    TIMESTAMPTZ,
    approved_by     BIGINT       REFERENCES users (id),
    approved_at     TIMESTAMPTZ,
    posted_by       BIGINT       REFERENCES users (id),
    posted_at       TIMESTAMPTZ,
    created_by      BIGINT       REFERENCES users (id),
    updated_by      BIGINT       REFERENCES users (id),
    version         INTEGER      NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT stock_documents_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT stock_documents_doc_type_check CHECK (doc_type IN ('adjustment', 'transfer', 'count')),
    CONSTRAINT stock_documents_status_check
        CHECK (status IN ('draft', 'pending', 'approved', 'posted', 'closed', 'voided')),
    CONSTRAINT stock_documents_transfer_target CHECK (
        (doc_type = 'transfer' AND to_warehouse_id IS NOT NULL AND to_warehouse_id <> warehouse_id)
        OR (doc_type <> 'transfer' AND to_warehouse_id IS NULL)),
    CONSTRAINT stock_documents_category_only_count CHECK (doc_type = 'count' OR category_id IS NULL)
);
CREATE INDEX stock_documents_company_type_date_idx ON stock_documents (company_id, doc_type, doc_date DESC);
-- 盤點凍結檢查用:進行中的盤點單
CREATE INDEX stock_documents_open_counts_idx ON stock_documents (company_id, warehouse_id)
    WHERE doc_type = 'count' AND status IN ('draft', 'pending', 'approved');
CREATE TRIGGER stock_documents_set_updated_at BEFORE UPDATE ON stock_documents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE stock_document_lines (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id BIGINT        NOT NULL REFERENCES stock_documents (id) ON DELETE CASCADE,
    line_no     INTEGER       NOT NULL,
    item_id     BIGINT        NOT NULL REFERENCES items (id),
    -- 輸入單位與數量;factor 為當下的換算倍數快照,base_qty = qty × factor(基本單位)
    -- 調整:qty 可正可負;調撥:qty > 0;盤點:qty 為實盤數(未盤為 NULL)
    unit_id     BIGINT        NOT NULL REFERENCES units (id),
    qty         NUMERIC(18,4),
    factor      NUMERIC(18,6) NOT NULL DEFAULT 1,
    base_qty    NUMERIC(18,4),
    -- 僅盤點:建立時的帳面數量快照(基本單位)
    system_qty  NUMERIC(18,4),
    note        VARCHAR(255)  NOT NULL DEFAULT '',
    CONSTRAINT stock_document_lines_doc_line_key UNIQUE (document_id, line_no),
    CONSTRAINT stock_document_lines_factor_positive CHECK (factor > 0)
);
CREATE INDEX stock_document_lines_item_id_idx ON stock_document_lines (item_id);

-- 現有量:料品 × 倉庫(基本單位);過帳時以 SELECT ... FOR UPDATE 鎖定
CREATE TABLE inventory_balances (
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    item_id      BIGINT        NOT NULL REFERENCES items (id),
    warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, warehouse_id)
);
CREATE INDEX inventory_balances_warehouse_idx ON inventory_balances (company_id, warehouse_id);

-- 庫存流水帳:唯一真相來源,只增不改;反過帳以反向分錄沖銷
CREATE TABLE inventory_transactions (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id     BIGINT        NOT NULL REFERENCES companies (id),
    item_id        BIGINT        NOT NULL REFERENCES items (id),
    warehouse_id   BIGINT        NOT NULL REFERENCES warehouses (id),
    doc_date       DATE          NOT NULL,                 -- 單據日期(報表依此)
    qty            NUMERIC(18,4) NOT NULL,                 -- 基本單位,正入負出
    -- 單位成本:M7 月結回寫;調整增加時可先填入
    unit_cost      NUMERIC(18,6),
    source_type    VARCHAR(30)   NOT NULL,                 -- 例:stock_adjustment、goods_receipt
    source_id      BIGINT        NOT NULL,
    source_line_id BIGINT,
    source_no      VARCHAR(30)   NOT NULL,
    reversal_of    BIGINT        REFERENCES inventory_transactions (id),
    created_by     BIGINT        REFERENCES users (id),
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT inventory_transactions_qty_nonzero CHECK (qty <> 0)
);
CREATE INDEX inventory_transactions_item_wh_date_idx ON inventory_transactions (item_id, warehouse_id, doc_date, id);
CREATE INDEX inventory_transactions_source_idx ON inventory_transactions (source_type, source_id);
CREATE INDEX inventory_transactions_company_date_idx ON inventory_transactions (company_id, doc_date);

-- 只允許月結回寫 unit_cost;其他欄位不可修改,也不可刪除
CREATE FUNCTION inventory_transactions_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' OR (to_jsonb(NEW) - 'unit_cost') <> (to_jsonb(OLD) - 'unit_cost') THEN
        RAISE EXCEPTION 'inventory_transactions 不可修改或刪除,請以反向分錄沖銷';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER inventory_transactions_no_update_delete BEFORE UPDATE OR DELETE ON inventory_transactions
    FOR EACH ROW EXECUTE FUNCTION inventory_transactions_immutable();
