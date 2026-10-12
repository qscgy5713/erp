-- 營業稅 401 每期的人工輸入:上期累積留抵稅額、海關代徵營業稅額、是否按比例扣抵(兼營免稅)
CREATE TABLE vat_period_settings (
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    year         INTEGER       NOT NULL,
    period       INTEGER       NOT NULL,
    prior_credit NUMERIC(18,4) NOT NULL DEFAULT 0,
    customs_tax  NUMERIC(18,4) NOT NULL DEFAULT 0,
    use_ratio    BOOLEAN       NOT NULL DEFAULT FALSE,
    updated_by   BIGINT        REFERENCES users (id),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, year, period),
    CONSTRAINT vat_period_settings_period_check CHECK (period BETWEEN 1 AND 6),
    CONSTRAINT vat_period_settings_amount_check CHECK (prior_credit >= 0 AND customs_tax >= 0)
);
