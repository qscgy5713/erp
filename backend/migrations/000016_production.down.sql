DELETE FROM account_mappings WHERE map_key = 'cost.absorb';
DELETE FROM accounts WHERE code = '5103' AND NOT EXISTS (SELECT 1 FROM voucher_lines l WHERE l.account_id = accounts.id);
DELETE FROM doc_number_rules WHERE doc_type = 'work_order';
ALTER TABLE item_costs DROP COLUMN consume_value, DROP COLUMN consume_qty;
DROP TABLE IF EXISTS work_order_lines;
DROP TABLE IF EXISTS work_orders;
DROP TABLE IF EXISTS bom_lines;
DROP TABLE IF EXISTS boms;
