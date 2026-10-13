DELETE FROM doc_number_rules WHERE doc_type IN ('work_order_wip_issue', 'work_order_wip_return');
ALTER TABLE work_order_completion_lines DROP COLUMN IF EXISTS from_wip;
DROP TABLE IF EXISTS work_order_wip_txn_lines;
DROP TABLE IF EXISTS work_order_wip_txns;
DROP INDEX IF EXISTS warehouses_wip_key;
ALTER TABLE warehouses DROP CONSTRAINT warehouses_kind_check;
ALTER TABLE warehouses ADD CONSTRAINT warehouses_kind_check CHECK (kind IN ('normal', 'subcontract'));
