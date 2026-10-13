DELETE FROM doc_number_rules WHERE doc_type IN ('subcontract_order', 'subcontract_issue', 'subcontract_receive', 'subcontract_return');
DROP TABLE IF EXISTS subcontract_txn_lines;
DROP TABLE IF EXISTS subcontract_txns;
DROP TABLE IF EXISTS subcontract_order_lines;
DROP TABLE IF EXISTS subcontract_orders;
DROP INDEX IF EXISTS warehouses_subcontract_supplier_key;
ALTER TABLE warehouses DROP CONSTRAINT IF EXISTS warehouses_kind_supplier_check, DROP CONSTRAINT IF EXISTS warehouses_kind_check,
    DROP COLUMN IF EXISTS supplier_id, DROP COLUMN IF EXISTS kind;
