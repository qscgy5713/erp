-- M5 應收應付:收款單 / 付款單沖帳

-- 收款單與付款單共用(D42):過帳時沖銷應收 / 應付的 paid_amount
CREATE TABLE settlements (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id    BIGINT        NOT NULL REFERENCES companies (id),
    -- receipt 收款(客戶) / payment 付款(供應商)
    side          VARCHAR(10)   NOT NULL,
    doc_no        VARCHAR(30)   NOT NULL,
    doc_date      DATE          NOT NULL,
    customer_id   BIGINT        REFERENCES customers (id),
    supplier_id   BIGINT        REFERENCES suppliers (id),
    sales_user_id BIGINT        REFERENCES users (id),     -- 收款:負責業務快照,資料範圍依此(D38)
    currency      CHAR(3)       NOT NULL REFERENCES currencies (code),
    method        VARCHAR(10)   NOT NULL DEFAULT 'transfer',
    reference     VARCHAR(50)   NOT NULL DEFAULT '',        -- 匯款帳號末碼 / 票號等
    amount        NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 沖帳明細合計(原幣)
    status        VARCHAR(20)   NOT NULL DEFAULT 'draft',
    note          TEXT          NOT NULL DEFAULT '',
    submitted_by  BIGINT        REFERENCES users (id),
    submitted_at  TIMESTAMPTZ,
    approved_by   BIGINT        REFERENCES users (id),
    approved_at   TIMESTAMPTZ,
    posted_by     BIGINT        REFERENCES users (id),
    posted_at     TIMESTAMPTZ,
    created_by    BIGINT        REFERENCES users (id),
    updated_by    BIGINT        REFERENCES users (id),
    version       INTEGER       NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT settlements_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT settlements_side_check CHECK (side IN ('receipt', 'payment')),
    CONSTRAINT settlements_partner_check CHECK (
        (side = 'receipt' AND customer_id IS NOT NULL AND supplier_id IS NULL)
        OR (side = 'payment' AND supplier_id IS NOT NULL AND customer_id IS NULL)),
    CONSTRAINT settlements_method_check CHECK (method IN ('cash', 'transfer', 'check', 'other')),
    CONSTRAINT settlements_status_check CHECK (status IN ('draft', 'pending', 'approved', 'posted', 'voided')),
    CONSTRAINT settlements_amount_check CHECK (amount >= 0)
);
CREATE INDEX settlements_company_side_date_idx ON settlements (company_id, side, doc_date DESC);
CREATE INDEX settlements_customer_idx ON settlements (customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX settlements_supplier_idx ON settlements (supplier_id) WHERE supplier_id IS NOT NULL;
CREATE TRIGGER settlements_set_updated_at BEFORE UPDATE ON settlements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 沖帳明細:每行沖一筆應收或應付。金額與該筆未沖餘額同號(退回 / 退出的負數可與正數互抵),不可超過餘額
CREATE TABLE settlement_lines (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    settlement_id BIGINT        NOT NULL REFERENCES settlements (id) ON DELETE CASCADE,
    line_no       INTEGER       NOT NULL,
    receivable_id BIGINT        REFERENCES accounts_receivable (id),
    payable_id    BIGINT        REFERENCES accounts_payable (id),
    amount        NUMERIC(18,4) NOT NULL,
    CONSTRAINT settlement_lines_line_key UNIQUE (settlement_id, line_no),
    CONSTRAINT settlement_lines_one_target CHECK ((receivable_id IS NULL) <> (payable_id IS NULL)),
    CONSTRAINT settlement_lines_amount_nonzero CHECK (amount <> 0)
);
CREATE UNIQUE INDEX settlement_lines_receivable_key ON settlement_lines (settlement_id, receivable_id)
    WHERE receivable_id IS NOT NULL;
CREATE UNIQUE INDEX settlement_lines_payable_key ON settlement_lines (settlement_id, payable_id)
    WHERE payable_id IS NOT NULL;
CREATE INDEX settlement_lines_receivable_idx ON settlement_lines (receivable_id) WHERE receivable_id IS NOT NULL;
CREATE INDEX settlement_lines_payable_idx ON settlement_lines (payable_id) WHERE payable_id IS NOT NULL;
