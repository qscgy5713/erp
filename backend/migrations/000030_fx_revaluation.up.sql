-- 外幣期末重估(D86):未實現兌換損益科目與重估紀錄
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '4903', '未實現兌換利益', 'revenue' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO accounts (company_id, code, name, acct_type)
SELECT c.id, '7103', '未實現兌換損失', 'expense' FROM companies c ON CONFLICT (company_id, code) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'fx.unrealized_gain', a.id FROM accounts a WHERE a.code = '4903'
ON CONFLICT (company_id, map_key) DO NOTHING;
INSERT INTO account_mappings (company_id, map_key, account_id)
SELECT a.company_id, 'fx.unrealized_loss', a.id FROM accounts a WHERE a.code = '7103'
ON CONFLICT (company_id, map_key) DO NOTHING;

CREATE TABLE fx_revaluations (
    id                       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id               BIGINT        NOT NULL REFERENCES companies (id),
    period                   VARCHAR(7)    NOT NULL,                   -- YYYY-MM
    status                   VARCHAR(10)   NOT NULL DEFAULT 'active',  -- active / undone
    ar_diff                  NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 應收重估差額(重估後 − 帳面,本位幣)
    ap_diff                  NUMERIC(18,4) NOT NULL DEFAULT 0,         -- 應付重估差額
    voucher_id               BIGINT        REFERENCES vouchers (id),   -- 差額為 0 時沒有傳票
    prev_revaluation_id      BIGINT        REFERENCES fx_revaluations (id), -- 本次一併沖回的上期重估
    created_by               BIGINT        REFERENCES users (id),
    created_at               TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT fx_revaluations_status_check CHECK (status IN ('active', 'undone')),
    CONSTRAINT fx_revaluations_period_check CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$')
);
CREATE UNIQUE INDEX fx_revaluations_active_key ON fx_revaluations (company_id, period) WHERE status = 'active';
