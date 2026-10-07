-- M1 基本資料:幣別匯率、稅別、付款條件、單位、料品分類、料品、倉庫、客戶、供應商

-- 幣別(ISO 4217,全系統共用)
CREATE TABLE currencies (
    code       CHAR(3)     PRIMARY KEY,
    name       VARCHAR(50) NOT NULL,
    symbol     VARCHAR(5)  NOT NULL DEFAULT '',
    -- 金額小數位數(TWD/JPY 為 0)
    decimals   SMALLINT    NOT NULL DEFAULT 2,
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    sort_order INTEGER     NOT NULL DEFAULT 0,
    CONSTRAINT currencies_code_format CHECK (code ~ '^[A-Z]{3}$'),
    CONSTRAINT currencies_decimals_check CHECK (decimals BETWEEN 0 AND 4)
);

INSERT INTO currencies (code, name, symbol, decimals, sort_order) VALUES
    ('TWD', '新台幣', 'NT$', 0, 1),
    ('USD', '美元', 'US$', 2, 2),
    ('JPY', '日圓', '¥', 0, 3),
    ('CNY', '人民幣', '¥', 2, 4),
    ('EUR', '歐元', '€', 2, 5),
    ('HKD', '港幣', 'HK$', 2, 6);

-- 匯率:1 單位外幣 = rate 本位幣;查詢時取「該日或之前最近一筆」
CREATE TABLE exchange_rates (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT        NOT NULL REFERENCES companies (id),
    currency    CHAR(3)       NOT NULL REFERENCES currencies (code),
    rate_date   DATE          NOT NULL,
    rate        NUMERIC(18,6) NOT NULL,
    created_by  BIGINT        REFERENCES users (id),
    updated_by  BIGINT        REFERENCES users (id),
    version     INTEGER       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT exchange_rates_company_currency_date_key UNIQUE (company_id, currency, rate_date),
    CONSTRAINT exchange_rates_rate_positive CHECK (rate > 0)
);
CREATE TRIGGER exchange_rates_set_updated_at BEFORE UPDATE ON exchange_rates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 稅別
CREATE TABLE tax_types (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    code        VARCHAR(10)  NOT NULL,
    name        VARCHAR(50)  NOT NULL,
    -- taxable 應稅 / zero 零稅率 / exempt 免稅(營業稅申報分類不同)
    kind        VARCHAR(10)  NOT NULL,
    rate        NUMERIC(6,4) NOT NULL,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by  BIGINT       REFERENCES users (id),
    updated_by  BIGINT       REFERENCES users (id),
    version     INTEGER      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT tax_types_company_code_key UNIQUE (company_id, code),
    CONSTRAINT tax_types_kind_check CHECK (kind IN ('taxable', 'zero', 'exempt')),
    CONSTRAINT tax_types_rate_check CHECK (rate >= 0 AND rate < 1),
    CONSTRAINT tax_types_non_taxable_zero_rate CHECK (kind = 'taxable' OR rate = 0)
);
CREATE TRIGGER tax_types_set_updated_at BEFORE UPDATE ON tax_types
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO tax_types (company_id, code, name, kind, rate)
SELECT c.id, t.code, t.name, t.kind, t.rate
FROM companies c
CROSS JOIN (VALUES
    ('TX5', '應稅 5%', 'taxable', 0.05),
    ('TX0', '零稅率', 'zero', 0),
    ('EX', '免稅', 'exempt', 0)
) AS t (code, name, kind, rate);

-- 付款條件:到期日 = (月結 ? 單據日所在月底 : 單據日) + net_days
CREATE TABLE payment_terms (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id   BIGINT      NOT NULL REFERENCES companies (id),
    code         VARCHAR(20) NOT NULL,
    name         VARCHAR(50) NOT NULL,
    is_month_end BOOLEAN     NOT NULL DEFAULT FALSE,
    net_days     INTEGER     NOT NULL DEFAULT 0,
    is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by   BIGINT      REFERENCES users (id),
    updated_by   BIGINT      REFERENCES users (id),
    version      INTEGER     NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_terms_company_code_key UNIQUE (company_id, code),
    CONSTRAINT payment_terms_net_days_check CHECK (net_days BETWEEN 0 AND 365)
);
CREATE TRIGGER payment_terms_set_updated_at BEFORE UPDATE ON payment_terms
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO payment_terms (company_id, code, name, is_month_end, net_days)
SELECT c.id, t.code, t.name, t.month_end, t.days
FROM companies c
CROSS JOIN (VALUES
    ('COD', '貨到付款', FALSE, 0),
    ('NET30', '發票日後 30 天', FALSE, 30),
    ('M0', '月結(當月底)', TRUE, 0),
    ('M30', '月結 30 天', TRUE, 30),
    ('M60', '月結 60 天', TRUE, 60)
) AS t (code, name, month_end, days);

-- 單位
CREATE TABLE units (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT      NOT NULL REFERENCES companies (id),
    code        VARCHAR(10) NOT NULL,
    name        VARCHAR(20) NOT NULL,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by  BIGINT      REFERENCES users (id),
    updated_by  BIGINT      REFERENCES users (id),
    version     INTEGER     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT units_company_code_key UNIQUE (company_id, code)
);
CREATE TRIGGER units_set_updated_at BEFORE UPDATE ON units
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO units (company_id, code, name)
SELECT c.id, u.code, u.name
FROM companies c
CROSS JOIN (VALUES
    ('PCS', '個'), ('BOX', '箱'), ('PKG', '包'), ('SET', '組'), ('DOZ', '打'),
    ('KG', '公斤'), ('G', '公克'), ('L', '公升'), ('M', '公尺'),
    ('EA', '件'), ('SHT', '張'), ('BTL', '瓶'), ('UNIT', '台')
) AS u (code, name);

-- 料品分類(樹狀)
CREATE TABLE item_categories (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    parent_id   BIGINT       REFERENCES item_categories (id),
    code        VARCHAR(20)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    sort_order  INTEGER      NOT NULL DEFAULT 0,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by  BIGINT       REFERENCES users (id),
    updated_by  BIGINT       REFERENCES users (id),
    version     INTEGER      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT item_categories_company_code_key UNIQUE (company_id, code),
    CONSTRAINT item_categories_not_self_parent CHECK (parent_id <> id)
);
CREATE INDEX item_categories_parent_id_idx ON item_categories (parent_id);
CREATE TRIGGER item_categories_set_updated_at BEFORE UPDATE ON item_categories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 倉庫
CREATE TABLE warehouses (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id     BIGINT       NOT NULL REFERENCES companies (id),
    code           VARCHAR(20)  NOT NULL,
    name           VARCHAR(100) NOT NULL,
    address        VARCHAR(255) NOT NULL DEFAULT '',
    -- 允許負庫存(D6:預設不允許)
    allow_negative BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by     BIGINT       REFERENCES users (id),
    updated_by     BIGINT       REFERENCES users (id),
    version        INTEGER      NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT warehouses_company_code_key UNIQUE (company_id, code)
);
CREATE TRIGGER warehouses_set_updated_at BEFORE UPDATE ON warehouses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 料品
CREATE TABLE items (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id           BIGINT        NOT NULL REFERENCES companies (id),
    code                 VARCHAR(40)   NOT NULL,                 -- 料號
    name                 VARCHAR(200)  NOT NULL,
    spec                 VARCHAR(255)  NOT NULL DEFAULT '',      -- 規格
    category_id          BIGINT        REFERENCES item_categories (id),
    -- goods 商品(有庫存)/ service 服務或費用(無庫存)
    item_type            VARCHAR(10)   NOT NULL DEFAULT 'goods',
    base_unit_id         BIGINT        NOT NULL REFERENCES units (id),
    barcode              VARCHAR(50),
    tax_type_id          BIGINT        REFERENCES tax_types (id),
    default_warehouse_id BIGINT        REFERENCES warehouses (id),
    safety_stock         NUMERIC(18,4) NOT NULL DEFAULT 0,       -- 以基本單位計
    list_price           NUMERIC(18,6) NOT NULL DEFAULT 0,       -- 建議售價(未稅,基本單位)
    note                 TEXT          NOT NULL DEFAULT '',
    is_active            BOOLEAN       NOT NULL DEFAULT TRUE,
    created_by           BIGINT        REFERENCES users (id),
    updated_by           BIGINT        REFERENCES users (id),
    version              INTEGER       NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT items_company_code_key UNIQUE (company_id, code),
    CONSTRAINT items_item_type_check CHECK (item_type IN ('goods', 'service')),
    CONSTRAINT items_safety_stock_check CHECK (safety_stock >= 0),
    CONSTRAINT items_list_price_check CHECK (list_price >= 0)
);
CREATE UNIQUE INDEX items_company_barcode_key ON items (company_id, barcode) WHERE barcode IS NOT NULL;
CREATE INDEX items_category_id_idx ON items (category_id);
CREATE TRIGGER items_set_updated_at BEFORE UPDATE ON items
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 料品的其他單位:1 個 unit = factor 個基本單位(例:1 箱 = 12 個)
CREATE TABLE item_units (
    item_id    BIGINT        NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    unit_id    BIGINT        NOT NULL REFERENCES units (id),
    factor     NUMERIC(18,6) NOT NULL,
    barcode    VARCHAR(50),
    PRIMARY KEY (item_id, unit_id),
    CONSTRAINT item_units_factor_positive CHECK (factor > 0)
);

-- 客戶
CREATE TABLE customers (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    code            VARCHAR(20)   NOT NULL,
    name            VARCHAR(200)  NOT NULL,
    short_name      VARCHAR(50)   NOT NULL DEFAULT '',
    tax_id          CHAR(8),                                   -- 統一編號
    invoice_title   VARCHAR(200)  NOT NULL DEFAULT '',         -- 發票抬頭
    phone           VARCHAR(50)   NOT NULL DEFAULT '',
    email           VARCHAR(255)  NOT NULL DEFAULT '',
    -- [{name, title, phone, email}]
    contacts        JSONB         NOT NULL DEFAULT '[]',
    -- [{label, zip, address, is_default}]
    addresses       JSONB         NOT NULL DEFAULT '[]',
    currency        CHAR(3)       NOT NULL DEFAULT 'TWD' REFERENCES currencies (code),
    tax_type_id     BIGINT        REFERENCES tax_types (id),
    payment_term_id BIGINT        REFERENCES payment_terms (id),
    credit_limit    NUMERIC(18,4) NOT NULL DEFAULT 0,          -- 0 表示不限
    sales_user_id   BIGINT        REFERENCES users (id),       -- 負責業務(資料範圍依此)
    note            TEXT          NOT NULL DEFAULT '',
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    created_by      BIGINT        REFERENCES users (id),
    updated_by      BIGINT        REFERENCES users (id),
    version         INTEGER       NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT customers_company_code_key UNIQUE (company_id, code),
    CONSTRAINT customers_tax_id_format CHECK (tax_id ~ '^[0-9]{8}$'),
    CONSTRAINT customers_credit_limit_check CHECK (credit_limit >= 0),
    CONSTRAINT customers_contacts_array CHECK (jsonb_typeof(contacts) = 'array'),
    CONSTRAINT customers_addresses_array CHECK (jsonb_typeof(addresses) = 'array')
);
CREATE INDEX customers_sales_user_id_idx ON customers (sales_user_id);
CREATE INDEX customers_tax_id_idx ON customers (company_id, tax_id);
CREATE TRIGGER customers_set_updated_at BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 供應商
CREATE TABLE suppliers (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT        NOT NULL REFERENCES companies (id),
    code            VARCHAR(20)   NOT NULL,
    name            VARCHAR(200)  NOT NULL,
    short_name      VARCHAR(50)   NOT NULL DEFAULT '',
    tax_id          CHAR(8),
    phone           VARCHAR(50)   NOT NULL DEFAULT '',
    email           VARCHAR(255)  NOT NULL DEFAULT '',
    contacts        JSONB         NOT NULL DEFAULT '[]',
    addresses       JSONB         NOT NULL DEFAULT '[]',
    currency        CHAR(3)       NOT NULL DEFAULT 'TWD' REFERENCES currencies (code),
    tax_type_id     BIGINT        REFERENCES tax_types (id),
    payment_term_id BIGINT        REFERENCES payment_terms (id),
    -- 匯款資訊
    bank_name       VARCHAR(100)  NOT NULL DEFAULT '',
    bank_account    VARCHAR(50)   NOT NULL DEFAULT '',
    note            TEXT          NOT NULL DEFAULT '',
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    created_by      BIGINT        REFERENCES users (id),
    updated_by      BIGINT        REFERENCES users (id),
    version         INTEGER       NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT suppliers_company_code_key UNIQUE (company_id, code),
    CONSTRAINT suppliers_tax_id_format CHECK (tax_id ~ '^[0-9]{8}$'),
    CONSTRAINT suppliers_contacts_array CHECK (jsonb_typeof(contacts) = 'array'),
    CONSTRAINT suppliers_addresses_array CHECK (jsonb_typeof(addresses) = 'array')
);
CREATE INDEX suppliers_tax_id_idx ON suppliers (company_id, tax_id);
CREATE TRIGGER suppliers_set_updated_at BEFORE UPDATE ON suppliers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
