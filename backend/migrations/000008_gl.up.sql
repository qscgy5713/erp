-- M6 會計總帳:科目、拋轉對照、傳票、會計期間

CREATE TABLE accounts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    code        VARCHAR(10)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    -- asset 資產 / liability 負債 / equity 權益 / revenue 收入 / cost 成本 / expense 費用
    acct_type   VARCHAR(10)  NOT NULL,
    parent_id   BIGINT       REFERENCES accounts (id),
    -- 只有明細科目可記帳;彙總科目(群組)不可
    is_postable BOOLEAN      NOT NULL DEFAULT TRUE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    note        VARCHAR(255) NOT NULL DEFAULT '',
    created_by  BIGINT       REFERENCES users (id),
    updated_by  BIGINT       REFERENCES users (id),
    version     INTEGER      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT accounts_company_code_key UNIQUE (company_id, code),
    CONSTRAINT accounts_code_format CHECK (code ~ '^[0-9]{3,10}$'),
    CONSTRAINT accounts_type_check CHECK (acct_type IN ('asset', 'liability', 'equity', 'revenue', 'cost', 'expense')),
    CONSTRAINT accounts_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX accounts_parent_idx ON accounts (parent_id);
CREATE TRIGGER accounts_set_updated_at BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 拋轉規則:業務事件的借貸科目對照(D46)。key 由程式定義,科目可在畫面調整
CREATE TABLE account_mappings (
    company_id BIGINT      NOT NULL REFERENCES companies (id),
    map_key    VARCHAR(40) NOT NULL,
    account_id BIGINT      NOT NULL REFERENCES accounts (id),
    updated_by BIGINT      REFERENCES users (id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, map_key)
);

CREATE TABLE vouchers (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    doc_no       VARCHAR(30)   NOT NULL,
    voucher_date DATE          NOT NULL,
    -- manual 手動 / 其餘為業務單據自動拋轉:goods_receipt、purchase_return、delivery、sales_return、collection、payment
    source_type  VARCHAR(30)   NOT NULL DEFAULT 'manual',
    source_id    BIGINT,
    source_no    VARCHAR(30)   NOT NULL DEFAULT '',
    description  VARCHAR(255)  NOT NULL DEFAULT '',
    status       VARCHAR(10)   NOT NULL DEFAULT 'draft',
    reversal_of  BIGINT        REFERENCES vouchers (id),   -- 沖銷傳票:指向被沖銷的傳票
    total_amount NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 借方(= 貸方)合計
    posted_by    BIGINT        REFERENCES users (id),
    posted_at    TIMESTAMPTZ,
    created_by   BIGINT        REFERENCES users (id),
    updated_by   BIGINT        REFERENCES users (id),
    version      INTEGER       NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT vouchers_company_doc_no_key UNIQUE (company_id, doc_no),
    CONSTRAINT vouchers_status_check CHECK (status IN ('draft', 'posted', 'voided')),
    CONSTRAINT vouchers_source_check CHECK ((source_type = 'manual') = (source_id IS NULL) OR reversal_of IS NOT NULL)
);
CREATE INDEX vouchers_company_date_idx ON vouchers (company_id, voucher_date, id);
CREATE INDEX vouchers_source_idx ON vouchers (source_type, source_id);
CREATE UNIQUE INDEX vouchers_reversal_key ON vouchers (reversal_of) WHERE reversal_of IS NOT NULL;
CREATE TRIGGER vouchers_set_updated_at BEFORE UPDATE ON vouchers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE voucher_lines (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voucher_id    BIGINT        NOT NULL REFERENCES vouchers (id),
    line_no       INTEGER       NOT NULL,
    account_id    BIGINT        NOT NULL REFERENCES accounts (id),
    debit         NUMERIC(18,4) NOT NULL DEFAULT 0,
    credit        NUMERIC(18,4) NOT NULL DEFAULT 0,
    description   VARCHAR(255)  NOT NULL DEFAULT '',
    -- 輔助核算
    customer_id   BIGINT        REFERENCES customers (id),
    supplier_id   BIGINT        REFERENCES suppliers (id),
    department_id BIGINT        REFERENCES departments (id),
    CONSTRAINT voucher_lines_voucher_line_key UNIQUE (voucher_id, line_no),
    CONSTRAINT voucher_lines_amount_check CHECK (debit >= 0 AND credit >= 0 AND (debit > 0) <> (credit > 0))
);
CREATE INDEX voucher_lines_account_idx ON voucher_lines (account_id);

-- 已過帳 / 作廢的傳票不可修改或刪除,更正以沖銷傳票處理
CREATE FUNCTION vouchers_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.status <> 'draft' THEN
        RAISE EXCEPTION '傳票不可刪除,已過帳或作廢的傳票不可修改,請以沖銷傳票更正';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER vouchers_no_change AFTER UPDATE OR DELETE ON vouchers
    FOR EACH ROW EXECUTE FUNCTION vouchers_immutable();

CREATE FUNCTION voucher_lines_immutable() RETURNS trigger AS $$
DECLARE st TEXT;
BEGIN
    SELECT status INTO st FROM vouchers WHERE id = OLD.voucher_id;
    IF st IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION '已過帳或作廢傳票的分錄不可修改或刪除';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER voucher_lines_no_change BEFORE UPDATE OR DELETE ON voucher_lines
    FOR EACH ROW EXECUTE FUNCTION voucher_lines_immutable();

-- 會計期間:沒有紀錄視為開放;關帳後該期不可再過帳 / 反過帳單據或新增傳票
CREATE TABLE accounting_periods (
    company_id  BIGINT      NOT NULL REFERENCES companies (id),
    period      CHAR(7)     NOT NULL,                       -- YYYY-MM
    status      VARCHAR(10) NOT NULL DEFAULT 'open',
    closed_by   BIGINT      REFERENCES users (id),
    closed_at   TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, period),
    CONSTRAINT accounting_periods_status_check CHECK (status IN ('open', 'closed')),
    CONSTRAINT accounting_periods_format CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$')
);

INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, a.code, a.name, a.acct_type
FROM companies c
CROSS JOIN (VALUES
    ('1101', '庫存現金', 'asset'),
    ('1102', '銀行存款', 'asset'),
    ('1131', '應收帳款', 'asset'),
    ('1141', '商品存貨', 'asset'),
    ('1151', '預付款項', 'asset'),
    ('1181', '進項稅額', 'asset'),
    ('2101', '應付帳款', 'liability'),
    ('2111', '銷項稅額', 'liability'),
    ('2121', '預收款項', 'liability'),
    ('2131', '應付費用', 'liability'),
    ('3101', '股本', 'equity'),
    ('3201', '保留盈餘(累積盈虧)', 'equity'),
    ('4101', '銷貨收入', 'revenue'),
    ('4102', '銷貨退回及折讓', 'revenue'),
    ('4901', '其他收入', 'revenue'),
    ('5101', '銷貨成本', 'cost'),
    ('6101', '薪資支出', 'expense'),
    ('6102', '租金支出', 'expense'),
    ('6103', '水電瓦斯費', 'expense'),
    ('6104', '文具用品', 'expense'),
    ('6105', '運費', 'expense'),
    ('6106', '勞務費', 'expense'),
    ('6199', '其他營業費用', 'expense')
) AS a (code, name, acct_type);

INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, m.map_key, a.id
FROM accounts a
JOIN (VALUES
    ('purchase.inventory', '1141'),
    ('purchase.expense', '6106'),
    ('purchase.input_tax', '1181'),
    ('purchase.payable', '2101'),
    ('sales.receivable', '1131'),
    ('sales.revenue', '4101'),
    ('sales.output_tax', '2111'),
    ('sales.return', '4102'),
    ('settle.cash', '1101'),
    ('settle.bank', '1102')
) AS m (map_key, code) ON m.code = a.code;
