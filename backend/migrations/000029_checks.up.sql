-- 票據管理(D83):收付款方式為「支票」時,過帳記入應收 / 應付票據並登錄票據;到期兌現時再轉入銀行存款
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '1121', '應收票據', 'asset' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '2141', '應付票據', 'liability' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'settle.notes_receivable', a.id FROM accounts a WHERE a.code = '1121'
ON CONFLICT (company_id, map_key) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'settle.notes_payable', a.id FROM accounts a WHERE a.code = '2141'
ON CONFLICT (company_id, map_key) DO NOTHING;

ALTER TABLE settlements ADD COLUMN check_due_date DATE; -- 支票到期日(方式為 check 時使用)

CREATE TABLE checks (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id    BIGINT        NOT NULL REFERENCES companies (id),
    side          VARCHAR(10)   NOT NULL,                          -- receipt 應收票據 / payment 應付票據
    settlement_id BIGINT        NOT NULL UNIQUE REFERENCES settlements (id),
    customer_id   BIGINT        REFERENCES customers (id),
    supplier_id   BIGINT        REFERENCES suppliers (id),
    check_no      VARCHAR(50)   NOT NULL DEFAULT '',
    amount        NUMERIC(18,4) NOT NULL,                          -- 本位幣
    due_date      DATE          NOT NULL,
    status        VARCHAR(10)   NOT NULL DEFAULT 'pending',        -- pending 未兌現 / cleared 已兌現
    cleared_date  DATE,
    cleared_by    BIGINT        REFERENCES users (id),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT checks_side_check CHECK (side IN ('receipt', 'payment')),
    CONSTRAINT checks_status_check CHECK (status IN ('pending', 'cleared'))
);
CREATE INDEX checks_due_idx ON checks (company_id, status, due_date);
