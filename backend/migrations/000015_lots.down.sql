ALTER TABLE stock_document_lines DROP COLUMN expiry_date, DROP COLUMN lot_no;
ALTER TABLE delivery_lines DROP COLUMN expiry_date, DROP COLUMN lot_no;
ALTER TABLE goods_receipt_lines DROP COLUMN expiry_date, DROP COLUMN lot_no;
DROP INDEX IF EXISTS inventory_transactions_lot_idx;
ALTER TABLE inventory_transactions DROP COLUMN lot_id;
DROP TABLE IF EXISTS inventory_lot_balances;
DROP TABLE IF EXISTS item_lots;
ALTER TABLE items DROP CONSTRAINT items_lot_control_check;
ALTER TABLE items DROP COLUMN lot_control;
