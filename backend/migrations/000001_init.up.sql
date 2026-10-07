-- 共用:自動更新 updated_at
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 公司(第一期單一公司,各表預留 company_id)
CREATE TABLE companies (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code        VARCHAR(20)  NOT NULL UNIQUE,
    name        VARCHAR(100) NOT NULL,
    tax_id      CHAR(8),                          -- 統一編號
    currency    CHAR(3)      NOT NULL DEFAULT 'TWD', -- 本位幣
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    version     INTEGER      NOT NULL DEFAULT 1,  -- 樂觀鎖
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT companies_tax_id_format CHECK (tax_id ~ '^[0-9]{8}$')
);

CREATE TRIGGER companies_set_updated_at
    BEFORE UPDATE ON companies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO companies (code, name) VALUES ('HQ', '預設公司');
