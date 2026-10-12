-- 標準成本(D91):成品的目標單位成本(基本單位),月結後與實際成本比較
CREATE TABLE item_standard_costs (
    item_id       BIGINT        PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    company_id    BIGINT        NOT NULL REFERENCES companies (id),
    standard_cost NUMERIC(18,6) NOT NULL,
    updated_by    BIGINT        REFERENCES users (id),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT item_standard_costs_check CHECK (standard_cost >= 0)
);
CREATE INDEX item_standard_costs_company_idx ON item_standard_costs (company_id);
