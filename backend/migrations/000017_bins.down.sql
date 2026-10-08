ALTER TABLE stock_documents DROP CONSTRAINT stock_documents_transfer_target;
ALTER TABLE stock_documents ADD CONSTRAINT stock_documents_transfer_target CHECK (
    (doc_type = 'transfer' AND to_warehouse_id IS NOT NULL AND to_warehouse_id <> warehouse_id)
    OR (doc_type <> 'transfer' AND to_warehouse_id IS NULL));
ALTER TABLE work_orders DROP COLUMN output_bin_code;
ALTER TABLE work_order_lines DROP COLUMN bin_code;
ALTER TABLE stock_document_lines DROP COLUMN to_bin_code, DROP COLUMN bin_code;
ALTER TABLE delivery_lines DROP COLUMN bin_code;
ALTER TABLE goods_receipt_lines DROP COLUMN bin_code;
DROP INDEX IF EXISTS inventory_transactions_bin_idx;
ALTER TABLE inventory_transactions DROP COLUMN bin_id;
DROP TABLE IF EXISTS inventory_bin_balances;
DROP TABLE IF EXISTS bins;
ALTER TABLE warehouses DROP COLUMN use_bins;
