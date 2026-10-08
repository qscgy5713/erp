-- M4 銷售:報價單 / 訂單、出貨單 / 銷貨退回單、應收帳款

-- 報價單與訂單共用(D37);皆不過帳。訂單核准後保留庫存(未出貨量),已出貨量由已過帳出貨單即時加總
CREATE TABLE sales_orders (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    -- quotation 報價單 / order 訂單
    doc_type        VARCHAR(10)   NOT NULL,
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    customer_id     BIGINT        NOT NULL REFERENCES customers (id),
    -- 負責業務快照(取自客戶),資料範圍依此判斷(D38)
    sales_user_id   BIGINT        REFERENCES users (id),
    warehouse_id    BIGINT        NOT NULL REFERENCES warehouses (id),   -- 出貨倉
    quotation_id    BIGINT        REFERENCES sales_orders (id),          -- 訂單:來源報價單
    valid_until     DATE,                                                -- 報價:有效期限
    delivery_date   DATE,                                                -- 訂單:預定出貨日
    customer_po_no  VARCHAR(50)   NOT NULL DEFAULT '',                   -- 客戶訂單號碼
    currency        CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate   NUMERIC(18,6) NOT NULL,
    tax_type_id     BIGINT        NOT NULL REFERENCES tax_types (id),
    tax_rate        NUMERIC(6,4)  NOT NULL,
    payment_term_id BIGINT        REFERENCES payment_terms (id),
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
    CONSTRAINT sales_orders_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT sales_orders_doc_type_check CHECK (doc_type IN ('quotation', 'order')),
    CONSTRAINT sales_orders_status_check CHECK (status IN ('draft', 'pending', 'approved', 'closed', 'voided')),
    CONSTRAINT sales_orders_rate_positive CHECK (exchange_rate > 0),
    CONSTRAINT sales_orders_quotation_only_order CHECK (doc_type = 'order' OR quotation_id IS NULL)
);
CREATE INDEX sales_orders_company_type_date_idx ON sales_orders (company_id, doc_type, doc_date DESC);
CREATE INDEX sales_orders_customer_idx ON sales_orders (customer_id, status);
CREATE INDEX sales_orders_quotation_idx ON sales_orders (quotation_id) WHERE quotation_id IS NOT NULL;
CREATE TRIGGER sales_orders_set_updated_at BEFORE UPDATE ON sales_orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE sales_order_lines (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   BIGINT        NOT NULL REFERENCES sales_orders (id) ON DELETE CASCADE,
    line_no    INTEGER       NOT NULL,
    item_id    BIGINT        NOT NULL REFERENCES items (id),
    unit_id    BIGINT        NOT NULL REFERENCES units (id),
    qty        NUMERIC(18,4) NOT NULL,
    factor     NUMERIC(18,6) NOT NULL,
    base_qty   NUMERIC(18,4) NOT NULL,
    unit_price NUMERIC(18,6) NOT NULL,           -- 原幣、未稅、每輸入單位
    amount     NUMERIC(18,4) NOT NULL,
    note       VARCHAR(255)  NOT NULL DEFAULT '',
    CONSTRAINT sales_order_lines_order_line_key UNIQUE (order_id, line_no),
    CONSTRAINT sales_order_lines_qty_positive CHECK (qty > 0),
    CONSTRAINT sales_order_lines_factor_positive CHECK (factor > 0),
    CONSTRAINT sales_order_lines_price_check CHECK (unit_price >= 0)
);
CREATE INDEX sales_order_lines_item_id_idx ON sales_order_lines (item_id);

-- 出貨單與銷貨退回單共用
CREATE TABLE deliveries (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    -- delivery 出貨 / return 銷貨退回
    doc_type        VARCHAR(10)   NOT NULL,
    doc_no          VARCHAR(30)   NOT NULL,
    doc_date        DATE          NOT NULL,
    customer_id     BIGINT        NOT NULL REFERENCES customers (id),
    sales_user_id   BIGINT        REFERENCES users (id),
    warehouse_id    BIGINT        NOT NULL REFERENCES warehouses (id),
    currency        CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate   NUMERIC(18,6) NOT NULL,
    tax_type_id     BIGINT        NOT NULL REFERENCES tax_types (id),
    tax_rate        NUMERIC(6,4)  NOT NULL,
    payment_term_id BIGINT        REFERENCES payment_terms (id),
    -- 發票(第一期手動登錄,任何未作廢狀態皆可登錄,D40)
    invoice_no      VARCHAR(10)   NOT NULL DEFAULT '',
    invoice_date    DATE,
    untaxed_amount  NUMERIC(18,4) NOT NULL DEFAULT 0,
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
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
    CONSTRAINT deliveries_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT deliveries_doc_type_check CHECK (doc_type IN ('delivery', 'return')),
    CONSTRAINT deliveries_status_check CHECK (status IN ('draft', 'pending', 'approved', 'posted', 'voided')),
    CONSTRAINT deliveries_rate_positive CHECK (exchange_rate > 0),
    CONSTRAINT deliveries_invoice_no_format CHECK (invoice_no = '' OR invoice_no ~ '^[A-Z]{2}[0-9]{8}$')
);
CREATE INDEX deliveries_company_type_date_idx ON deliveries (company_id, doc_type, doc_date DESC);
CREATE INDEX deliveries_customer_idx ON deliveries (customer_id, status);
-- 同一發票號碼只能用在一張未作廢的單據
CREATE UNIQUE INDEX deliveries_invoice_no_key ON deliveries (company_id, invoice_no)
    WHERE invoice_no <> '' AND status <> 'voided';
CREATE TRIGGER deliveries_set_updated_at BEFORE UPDATE ON deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE delivery_lines (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id      BIGINT        NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
    line_no          INTEGER       NOT NULL,
    item_id          BIGINT        NOT NULL REFERENCES items (id),
    unit_id          BIGINT        NOT NULL REFERENCES units (id),
    qty              NUMERIC(18,4) NOT NULL,
    factor           NUMERIC(18,6) NOT NULL,
    base_qty         NUMERIC(18,4) NOT NULL,
    unit_price       NUMERIC(18,6) NOT NULL,
    amount           NUMERIC(18,4) NOT NULL,
    base_amount      NUMERIC(18,4) NOT NULL,
    -- 來源:出貨 → 訂單明細;退回 → 出貨明細(只有作廢單據會留下指向已刪除明細的參照)
    so_line_id       BIGINT        REFERENCES sales_order_lines (id) ON DELETE SET NULL,
    delivery_line_id BIGINT        REFERENCES delivery_lines (id) ON DELETE SET NULL,
    note             VARCHAR(255)  NOT NULL DEFAULT '',
    CONSTRAINT delivery_lines_delivery_line_key UNIQUE (delivery_id, line_no),
    CONSTRAINT delivery_lines_qty_positive CHECK (qty > 0),
    CONSTRAINT delivery_lines_factor_positive CHECK (factor > 0),
    CONSTRAINT delivery_lines_price_check CHECK (unit_price >= 0),
    CONSTRAINT delivery_lines_one_source CHECK (so_line_id IS NULL OR delivery_line_id IS NULL)
);
CREATE INDEX delivery_lines_item_id_idx ON delivery_lines (item_id);
CREATE INDEX delivery_lines_so_line_idx ON delivery_lines (so_line_id) WHERE so_line_id IS NOT NULL;
CREATE INDEX delivery_lines_delivery_line_idx ON delivery_lines (delivery_line_id) WHERE delivery_line_id IS NOT NULL;

-- 應收帳款:出貨 / 退回過帳時產生(退回為負數),反過帳時刪除
CREATE TABLE accounts_receivable (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id    BIGINT        NOT NULL REFERENCES companies (id),
    customer_id   BIGINT        NOT NULL REFERENCES customers (id),
    source_type   VARCHAR(30)   NOT NULL,                   -- delivery / sales_return
    source_id     BIGINT        NOT NULL,
    source_no     VARCHAR(30)   NOT NULL,
    doc_date      DATE          NOT NULL,
    due_date      DATE          NOT NULL,
    currency      CHAR(3)       NOT NULL REFERENCES currencies (code),
    exchange_rate NUMERIC(18,6) NOT NULL,
    amount        NUMERIC(18,4) NOT NULL,                   -- 原幣含稅
    base_amount   NUMERIC(18,4) NOT NULL,                   -- 本位幣含稅
    paid_amount   NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 已沖帳(原幣),M5 收款沖帳更新
    created_by    BIGINT        REFERENCES users (id),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT accounts_receivable_source_key UNIQUE (source_type, source_id)
);
CREATE INDEX accounts_receivable_customer_idx ON accounts_receivable (company_id, customer_id, due_date);
CREATE TRIGGER accounts_receivable_set_updated_at BEFORE UPDATE ON accounts_receivable
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
