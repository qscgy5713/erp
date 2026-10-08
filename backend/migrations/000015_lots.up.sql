-- 批號與效期管理(D63)
-- 料品的批號管理:none 不管理 / lot 管理批號 / lot_expiry 管理批號與效期
ALTER TABLE items ADD COLUMN lot_control VARCHAR(12) NOT NULL DEFAULT 'none';
ALTER TABLE items ADD CONSTRAINT items_lot_control_check CHECK (lot_control IN ('none', 'lot', 'lot_expiry'));

-- 批號主檔:同一料品的批號不重複;一個批號只有一個效期
CREATE TABLE item_lots (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT      NOT NULL REFERENCES companies (id),
    item_id     BIGINT      NOT NULL REFERENCES items (id),
    lot_no      VARCHAR(40) NOT NULL,
    expiry_date DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT item_lots_key UNIQUE (company_id, item_id, lot_no),
    CONSTRAINT item_lots_no_check CHECK (lot_no <> '')
);
CREATE INDEX item_lots_expiry_idx ON item_lots (company_id, expiry_date) WHERE expiry_date IS NOT NULL;

-- 批號現有量:只存批號管理料品,各批號合計必等於 inventory_balances 的該料品該倉現有量;批號庫存不可為負
CREATE TABLE inventory_lot_balances (
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    item_id      BIGINT        NOT NULL REFERENCES items (id),
    warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),
    lot_id       BIGINT        NOT NULL REFERENCES item_lots (id),
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (lot_id, warehouse_id),
    CONSTRAINT inventory_lot_balances_qty_check CHECK (qty >= 0)
);
CREATE INDEX inventory_lot_balances_item_wh_idx ON inventory_lot_balances (item_id, warehouse_id);

ALTER TABLE inventory_transactions ADD COLUMN lot_id BIGINT REFERENCES item_lots (id);
CREATE INDEX inventory_transactions_lot_idx ON inventory_transactions (lot_id, id) WHERE lot_id IS NOT NULL;

-- 單據明細上輸入的批號與效期:入庫(進貨、銷貨退回、調整增加)為批號與效期;出庫可指定批號,空白則先到期先出
ALTER TABLE goods_receipt_lines ADD COLUMN lot_no VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE goods_receipt_lines ADD COLUMN expiry_date DATE;
ALTER TABLE delivery_lines ADD COLUMN lot_no VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE delivery_lines ADD COLUMN expiry_date DATE;
ALTER TABLE stock_document_lines ADD COLUMN lot_no VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE stock_document_lines ADD COLUMN expiry_date DATE;
