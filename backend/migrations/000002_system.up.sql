-- 系統基礎:部門、使用者、角色權限、Refresh Token、稽核日誌、單號規則

CREATE TABLE departments (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    parent_id   BIGINT       REFERENCES departments (id),
    code        VARCHAR(20)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    sort_order  INTEGER      NOT NULL DEFAULT 0,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    version     INTEGER      NOT NULL DEFAULT 1,
    created_by  BIGINT,
    updated_by  BIGINT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT departments_company_code_key UNIQUE (company_id, code),
    CONSTRAINT departments_not_self_parent CHECK (parent_id <> id)
);
CREATE INDEX departments_parent_id_idx ON departments (parent_id);
CREATE TRIGGER departments_set_updated_at BEFORE UPDATE ON departments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE users (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id           BIGINT       NOT NULL REFERENCES companies (id),
    department_id        BIGINT       REFERENCES departments (id),
    username             VARCHAR(50)  NOT NULL,           -- 登入帳號
    name                 VARCHAR(100) NOT NULL,
    email                VARCHAR(255),
    password_hash        TEXT         NOT NULL,
    is_superadmin        BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active            BOOLEAN      NOT NULL DEFAULT TRUE,
    must_change_password BOOLEAN      NOT NULL DEFAULT FALSE,
    failed_login_count   INTEGER      NOT NULL DEFAULT 0,
    locked_until         TIMESTAMPTZ,
    last_login_at        TIMESTAMPTZ,
    password_changed_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- 改密碼、停用、重設密碼時遞增;access token 帶此值,不符即失效
    token_version        INTEGER      NOT NULL DEFAULT 1,
    version              INTEGER      NOT NULL DEFAULT 1,
    created_by           BIGINT,
    updated_by           BIGINT,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT users_username_format CHECK (username ~ '^[A-Za-z0-9._-]{3,50}$')
);
-- 登入時不選公司,帳號全域唯一(不分大小寫)
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
CREATE INDEX users_department_id_idx ON users (department_id);
CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE departments
    ADD CONSTRAINT departments_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id),
    ADD CONSTRAINT departments_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES users (id);
ALTER TABLE users
    ADD CONSTRAINT users_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id),
    ADD CONSTRAINT users_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES users (id);

CREATE TABLE roles (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    code        VARCHAR(30)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    -- 資料範圍:all 全部 / department 本部門 / self 本人;多角色取最大
    data_scope  VARCHAR(20)  NOT NULL DEFAULT 'self',
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    version     INTEGER      NOT NULL DEFAULT 1,
    created_by  BIGINT       REFERENCES users (id),
    updated_by  BIGINT       REFERENCES users (id),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT roles_company_code_key UNIQUE (company_id, code),
    CONSTRAINT roles_data_scope_check CHECK (data_scope IN ('all', 'department', 'self'))
);
CREATE TRIGGER roles_set_updated_at BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 權限點定義在程式碼(internal/system/permission),這裡只存代碼
CREATE TABLE role_permissions (
    role_id    BIGINT       NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission VARCHAR(100) NOT NULL,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE user_roles (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX user_roles_role_id_idx ON user_roles (role_id);

-- Refresh Token:只存 SHA-256;輪替時同一 family 串起來,偵測到重放就整串撤銷
CREATE TABLE refresh_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id   UUID        NOT NULL,
    token_hash  BYTEA       NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    ip          VARCHAR(64) NOT NULL DEFAULT '',
    user_agent  VARCHAR(255) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);

-- 稽核日誌:只增不改
CREATE TABLE audit_logs (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    user_id     BIGINT       REFERENCES users (id),
    action      VARCHAR(30)  NOT NULL,
    entity_type VARCHAR(50)  NOT NULL,
    entity_id   BIGINT,
    summary     VARCHAR(255) NOT NULL DEFAULT '',
    before_data JSONB,
    after_data  JSONB,
    ip          VARCHAR(64)  NOT NULL DEFAULT '',
    user_agent  VARCHAR(255) NOT NULL DEFAULT '',
    request_id  VARCHAR(64)  NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity_type, entity_id);
CREATE INDEX audit_logs_company_created_idx ON audit_logs (company_id, created_at DESC);
CREATE INDEX audit_logs_user_id_idx ON audit_logs (user_id);

CREATE FUNCTION audit_logs_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs 不可修改或刪除';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER audit_logs_no_update_delete BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

-- 單號規則:前綴 + 日期 + 流水號,流水號依日期格式的期間重置
CREATE TABLE doc_number_rules (
    company_id  BIGINT      NOT NULL REFERENCES companies (id),
    doc_type    VARCHAR(30) NOT NULL,
    name        VARCHAR(50) NOT NULL,
    prefix      VARCHAR(10) NOT NULL,
    date_format VARCHAR(10) NOT NULL DEFAULT 'YYYYMMDD',
    seq_length  INTEGER     NOT NULL DEFAULT 4,
    version     INTEGER     NOT NULL DEFAULT 1,
    updated_by  BIGINT      REFERENCES users (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, doc_type),
    CONSTRAINT doc_number_rules_date_format_check CHECK (date_format IN ('YYYYMMDD', 'YYYYMM', 'YYYY', 'NONE')),
    CONSTRAINT doc_number_rules_seq_length_check CHECK (seq_length BETWEEN 3 AND 10),
    CONSTRAINT doc_number_rules_prefix_format CHECK (prefix ~ '^[A-Z0-9]{1,10}$')
);
CREATE TRIGGER doc_number_rules_set_updated_at BEFORE UPDATE ON doc_number_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE doc_number_counters (
    company_id BIGINT      NOT NULL,
    doc_type   VARCHAR(30) NOT NULL,
    period_key VARCHAR(8)  NOT NULL,   -- 依 date_format 產生,NONE 時為空字串
    last_value BIGINT      NOT NULL,
    PRIMARY KEY (company_id, doc_type, period_key),
    FOREIGN KEY (company_id, doc_type) REFERENCES doc_number_rules (company_id, doc_type) ON DELETE CASCADE
);

INSERT INTO doc_number_rules (company_id, doc_type, name, prefix)
SELECT c.id, r.doc_type, r.name, r.prefix
FROM companies c
CROSS JOIN (VALUES
    ('purchase_order',    '採購單',     'PO'),
    ('goods_receipt',     '進貨單',     'GR'),
    ('purchase_return',   '進貨退出單', 'PT'),
    ('sales_quotation',   '報價單',     'QT'),
    ('sales_order',       '訂單',       'SO'),
    ('delivery',          '出貨單',     'DN'),
    ('sales_return',      '銷貨退回單', 'SR'),
    ('stock_adjustment',  '庫存調整單', 'IA'),
    ('stock_transfer',    '調撥單',     'IT'),
    ('stock_count',       '盤點單',     'IC'),
    ('receipt',           '收款單',     'RC'),
    ('payment',           '付款單',     'PM'),
    ('journal_voucher',   '傳票',       'JV')
) AS r (doc_type, name, prefix);
