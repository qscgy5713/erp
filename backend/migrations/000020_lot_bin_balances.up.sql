-- 批號 × 儲位庫存(D70):啟用儲位的倉庫,批號管理料品的庫存同時記錄在哪個儲位。
-- 不變式:同一(批號, 倉庫)各儲位合計 = 批號現有量;同一(料品, 儲位)各批號合計 = 儲位現有量。
CREATE TABLE inventory_lot_bin_balances (
    company_id   BIGINT        NOT NULL REFERENCES companies (id),
    item_id      BIGINT        NOT NULL REFERENCES items (id),
    warehouse_id BIGINT        NOT NULL REFERENCES warehouses (id),
    lot_id       BIGINT        NOT NULL REFERENCES item_lots (id),
    bin_id       BIGINT        NOT NULL REFERENCES bins (id),
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (lot_id, bin_id),
    CONSTRAINT inventory_lot_bin_balances_qty_check CHECK (qty >= 0)
);
CREATE INDEX inventory_lot_bin_balances_item_wh_idx ON inventory_lot_bin_balances (item_id, warehouse_id);
CREATE INDEX inventory_lot_bin_balances_bin_idx ON inventory_lot_bin_balances (bin_id, item_id);

-- 既有庫存的回填:先前批號與儲位各自獨立記錄,無法得知實際對應。
-- 以「西北角法」推算一個同時符合兩邊合計的對應(批號依效期、儲位依代號),實際擺放請以盤點修正。
DO $$
DECLARE
    p        RECORD;
    lot_ids  BIGINT[];
    lot_qty  NUMERIC[];
    bin_ids  BIGINT[];
    bin_qty  NUMERIC[];
    i        INT;
    j        INT;
    take     NUMERIC;
BEGIN
    FOR p IN
        SELECT DISTINCT b.company_id, b.item_id, b.warehouse_id
        FROM inventory_lot_balances b JOIN warehouses w ON w.id = b.warehouse_id
        WHERE w.use_bins AND b.qty > 0
    LOOP
        lot_ids := ARRAY(SELECT b.lot_id FROM inventory_lot_balances b JOIN item_lots l ON l.id = b.lot_id
                         WHERE b.item_id = p.item_id AND b.warehouse_id = p.warehouse_id AND b.qty > 0
                         ORDER BY l.expiry_date NULLS LAST, l.id);
        lot_qty := ARRAY(SELECT b.qty FROM inventory_lot_balances b JOIN item_lots l ON l.id = b.lot_id
                         WHERE b.item_id = p.item_id AND b.warehouse_id = p.warehouse_id AND b.qty > 0
                         ORDER BY l.expiry_date NULLS LAST, l.id);
        bin_ids := ARRAY(SELECT bb.bin_id FROM inventory_bin_balances bb JOIN bins bn ON bn.id = bb.bin_id
                         WHERE bb.item_id = p.item_id AND bb.warehouse_id = p.warehouse_id AND bb.qty > 0
                         ORDER BY bn.code);
        bin_qty := ARRAY(SELECT bb.qty FROM inventory_bin_balances bb JOIN bins bn ON bn.id = bb.bin_id
                         WHERE bb.item_id = p.item_id AND bb.warehouse_id = p.warehouse_id AND bb.qty > 0
                         ORDER BY bn.code);
        i := 1;
        j := 1;
        WHILE i <= COALESCE(array_length(lot_ids, 1), 0) AND j <= COALESCE(array_length(bin_ids, 1), 0) LOOP
            take := LEAST(lot_qty[i], bin_qty[j]);
            INSERT INTO inventory_lot_bin_balances (company_id, item_id, warehouse_id, lot_id, bin_id, qty)
            VALUES (p.company_id, p.item_id, p.warehouse_id, lot_ids[i], bin_ids[j], take);
            lot_qty[i] := lot_qty[i] - take;
            bin_qty[j] := bin_qty[j] - take;
            IF lot_qty[i] = 0 THEN i := i + 1; END IF;
            IF bin_qty[j] = 0 THEN j := j + 1; END IF;
        END LOOP;
    END LOOP;
END $$;
