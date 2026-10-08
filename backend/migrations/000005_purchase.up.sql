-- M3 採購:採購單、進貨單 / 進貨退出單、應付帳款

-- 採購單:核准後即可轉進貨;不過帳。已交量由已過帳的進貨單即時加總(D32)
CREATE TABLE purchase_orders (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    supplier_id     BIGINT        NOT NULL REFERENCES suppliers (id),
    warehouse_id    BIGINT        NOT NULL REFERENCES warehouses (id),   -- 預定入庫倉
    expected_date   DATE,                                                -- 預定交貨日
    currency        CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate   NUMERIC(18,6) NOT NULL,
    tax_type_id     BIGINT        NOT NULL REFERENCES tax_types (id),
    tax_rate        NUMERIC(6,4)  NOT NULL,                              -- 稅率快照
    payment_term_id BIGINT        REFERENCES payment_terms (id),
    -- 原幣金額(單價未稅)
    untaxed_amount  NUMERIC(18,4) NOT NULL DEFAULT 0,
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    status          VARCHAR(20)   NOT NULL DEFAULT 'draft',
    note            TEXT          NOT NULL DEFAULT '',
    submitted_by    BIGINT        REFERENCES users (id),
    submitted_at    TIMESTAMPTZ,
    approved_by     BIGINT        REFERENCES users (id),
    approved_at     TIMESTAMPTZ,
    closed_by       BIGINT        REFERENCES users (id),
    closed_at       TIMESTAMPTZ,
    created_by      BIGINT        REFERENCES users (id),
    updated_by      BIGINT        REFERENCES users (id),
    version         INTEGER       NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT purchase_orders_company_doc_no_key UNIQUE (company_id, doc_no),
    -- 採購單不過帳:沒有 posted
    CONSTRAINT purchase_orders_status_check CHECK (status IN ('draft', 'pending', 'approved', 'closed', 'voided')),
    CONSTRAINT purchase_orders_rate_positive CHECK (exchange_rate > 0)
);
CREATE INDEX purchase_orders_company_date_idx ON purchase_orders (company_id, doc_date DESC);
CREATE INDEX purchase_orders_supplier_idx ON purchase_orders (supplier_id, status);
CREATE TRIGGER purchase_orders_set_updated_at BEFORE UPDATE ON purchase_orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE purchase_order_lines (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   BIGINT        NOT NULL REFERENCES purchase_orders (id) ON DELETE CASCADE,
    line_no    INTEGER       NOT NULL,
    item_id    BIGINT        NOT NULL REFERENCES items (id),
    unit_id    BIGINT        NOT NULL REFERENCES units (id),
    qty        NUMERIC(18,4) NOT NULL,
    factor     NUMERIC(18,6) NOT NULL,
    base_qty   NUMERIC(18,4) NOT NULL,
    unit_price NUMERIC(18,6) NOT NULL,           -- 原幣、未稅、每輸入單位
    amount     NUMERIC(18,4) NOT NULL,           -- 依幣別小數位捨入
    note       VARCHAR(255)  NOT NULL DEFAULT '',
    CONSTRAINT purchase_order_lines_order_line_key UNIQUE (order_id, line_no),
    CONSTRAINT purchase_order_lines_qty_positive CHECK (qty > 0),
    CONSTRAINT purchase_order_lines_factor_positive CHECK (factor > 0),
    CONSTRAINT purchase_order_lines_price_check CHECK (unit_price >= 0)
);
CREATE INDEX purchase_order_lines_item_id_idx ON purchase_order_lines (item_id);

-- 進貨單與進貨退出單共用(D31)
CREATE TABLE goods_receipts (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    -- receipt 進貨 / return 進貨退出
    doc_type        VARCHAR(10)   NOT NULL,
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    supplier_id     BIGINT        NOT NULL REFERENCES suppliers (id),
    warehouse_id    BIGINT        NOT NULL REFERENCES warehouses (id),
    currency        CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate   NUMERIC(18,6) NOT NULL,
    tax_type_id     BIGINT        NOT NULL REFERENCES tax_types (id),
    tax_rate        NUMERIC(6,4)  NOT NULL,
    payment_term_id BIGINT        REFERENCES payment_terms (id),
    invoice_no      VARCHAR(20)   NOT NULL DEFAULT '',                  -- 供應商發票號碼
    -- 原幣金額
    untaxed_amount  NUMERIC(18,4) NOT NULL DEFAULT 0,
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    -- 本位幣金額(未稅 = 明細本位幣金額合計,與入庫成本一致)
    base_untaxed    NUMERIC(18,4) NOT NULL DEFAULT 0,
    base_tax        NUMERIC(18,4) NOT NULL DEFAULT 0,
    base_total      NUMERIC(18,4) NOT NULL DEFAULT 0,
    status          VARCHAR(20)   NOT NULL DEFAULT 'draft',
    note            TEXT          NOT NULL DEFAULT '',
    submitted_by    BIGINT        REFERENCES users (id),
    submitted_at    TIMESTAMPTZ,
    approved_by     BIGINT        REFERENCES users (id),
    approved_at     TIMESTAMPTZ,
    posted_by       BIGINT        REFERENCES users (id),
    posted_at       TIMESTAMPTZ,
    created_by      BIGINT        REFERENCES users (id),
    updated_by      BIGINT        REFERENCES users (id),
    version         INTEGER       NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT goods_receipts_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT goods_receipts_doc_type_check CHECK (doc_type IN ('receipt', 'return')),
    CONSTRAINT goods_receipts_status_check CHECK (status IN ('draft', 'pending', 'approved', 'posted', 'voided')),
    CONSTRAINT goods_receipts_rate_positive CHECK (exchange_rate > 0)
);
CREATE INDEX goods_receipts_company_type_date_idx ON goods_receipts (company_id, doc_type, doc_date DESC);
CREATE INDEX goods_receipts_supplier_idx ON goods_receipts (supplier_id, status);
CREATE TRIGGER goods_receipts_set_updated_at BEFORE UPDATE ON goods_receipts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE goods_receipt_lines (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    receipt_id      BIGINT        NOT NULL REFERENCES goods_receipts (id) ON DELETE CASCADE,
    line_no         INTEGER       NOT NULL,
    item_id         BIGINT        NOT NULL REFERENCES items (id),
    unit_id         BIGINT        NOT NULL REFERENCES units (id),
    qty             NUMERIC(18,4) NOT NULL,                 -- 一律為正;退出由 doc_type 決定方向
    factor          NUMERIC(18,6) NOT NULL,
    base_qty        NUMERIC(18,4) NOT NULL,
    unit_price      NUMERIC(18,6) NOT NULL,
    amount          NUMERIC(18,4) NOT NULL,                 -- 原幣
    base_amount     NUMERIC(18,4) NOT NULL,                 -- 本位幣,入庫成本依此
    -- 來源:進貨 → 採購單明細;退出 → 進貨單明細。
    -- 只有作廢單據會留下指向已刪除明細的參照,故以 SET NULL 處理
    po_line_id      BIGINT        REFERENCES purchase_order_lines (id) ON DELETE SET NULL,
    receipt_line_id BIGINT        REFERENCES goods_receipt_lines (id) ON DELETE SET NULL,
    note            VARCHAR(255)  NOT NULL DEFAULT '',
    CONSTRAINT goods_receipt_lines_receipt_line_key UNIQUE (receipt_id, line_no),
    CONSTRAINT goods_receipt_lines_qty_positive CHECK (qty > 0),
    CONSTRAINT goods_receipt_lines_factor_positive CHECK (factor > 0),
    CONSTRAINT goods_receipt_lines_price_check CHECK (unit_price >= 0),
    CONSTRAINT goods_receipt_lines_one_source CHECK (po_line_id IS NULL OR receipt_line_id IS NULL)
);
CREATE INDEX goods_receipt_lines_item_id_idx ON goods_receipt_lines (item_id);
CREATE INDEX goods_receipt_lines_po_line_idx ON goods_receipt_lines (po_line_id) WHERE po_line_id IS NOT NULL;
CREATE INDEX goods_receipt_lines_receipt_line_idx ON goods_receipt_lines (receipt_line_id)
    WHERE receipt_line_id IS NOT NULL;

-- 應付帳款:進貨 / 退出過帳時產生(退出為負數),反過帳時刪除(D34)
CREATE TABLE accounts_payable (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id    BIGINT        NOT NULL REFERENCES companies (id),
    supplier_id   BIGINT        NOT NULL REFERENCES suppliers (id),
    source_type   VARCHAR(30)   NOT NULL,                   -- goods_receipt / purchase_return
    source_id     BIGINT        NOT NULL,
    source_no     VARCHAR(30)   NOT NULL,
    doc_date      DATE          NOT NULL,
    due_date      DATE          NOT NULL,
    currency      CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate NUMERIC(18,6) NOT NULL,
    amount        NUMERIC(18,4) NOT NULL,                   -- 原幣含稅
    base_amount   NUMERIC(18,4) NOT NULL,                   -- 本位幣含稅
    paid_amount   NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 已沖帳(原幣),M5 付款沖帳更新
    created_by    BIGINT        REFERENCES users (id),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT accounts_payable_source_key UNIQUE (source_type, source_id)
);
CREATE INDEX accounts_payable_supplier_idx ON accounts_payable (company_id, supplier_id, due_date);
CREATE TRIGGER accounts_payable_set_updated_at BEFORE UPDATE ON accounts_payable
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
