-- 儲位(D65):倉庫可啟用儲位,啟用後所有庫存異動都須指定儲位(出庫可留空由系統分配)
ALTER TABLE warehouses ADD COLUMN use_bins BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE bins (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id   BIGINT       NOT NULL REFERENCES companies (id),
    warehouse_id BIGINT       NOT NULL REFERENCES warehouses (id),
    code         VARCHAR(20)  NOT NULL,
    name         VARCHAR(100) NOT NULL DEFAULT '',
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by   BIGINT       REFERENCES users (id),
    updated_by   BIGINT       REFERENCES users (id),
    version      INTEGER      NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT bins_warehouse_code_key UNIQUE (warehouse_id, code),
    CONSTRAINT bins_code_format CHECK (code ~ '^[A-Z0-9._-]{1,20}$')
);
CREATE TRIGGER bins_set_updated_at BEFORE UPDATE ON bins FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 儲位現有量:只存啟用儲位的倉庫;各儲位合計必等於該倉庫該料品的現有量;儲位庫存不可為負
CREATE TABLE inventory_bin_balances (
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    item_id      BIGINT        NOT NULL REFERENCES items (id),
    warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),
    bin_id       BIGINT        NOT NULL REFERENCES bins (id),
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (bin_id, item_id),
    CONSTRAINT inventory_bin_balances_qty_check CHECK (qty >= 0)
);
CREATE INDEX inventory_bin_balances_item_wh_idx ON inventory_bin_balances (item_id, warehouse_id);

ALTER TABLE inventory_transactions ADD COLUMN bin_id BIGINT REFERENCES bins (id);
CREATE INDEX inventory_transactions_bin_idx ON inventory_transactions (bin_id, id) WHERE bin_id IS NOT NULL;

-- 單據明細上輸入的儲位代號
ALTER TABLE goods_receipt_lines ADD COLUMN bin_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE delivery_lines ADD COLUMN bin_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE stock_document_lines ADD COLUMN bin_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE stock_document_lines ADD COLUMN to_bin_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE work_order_lines ADD COLUMN bin_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE work_orders ADD COLUMN output_bin_code VARCHAR(20) NOT NULL DEFAULT '';

-- 調撥:啟用儲位的倉庫可在同一倉庫的不同儲位之間調撥(來源與目的儲位的檢查在程式裡)
ALTER TABLE stock_documents DROP CONSTRAINT stock_documents_transfer_target;
ALTER TABLE stock_documents ADD CONSTRAINT stock_documents_transfer_target CHECK (
    (doc_type = 'transfer' AND to_warehouse_id IS NOT NULL) OR (doc_type <> 'transfer' AND to_warehouse_id IS NULL));
