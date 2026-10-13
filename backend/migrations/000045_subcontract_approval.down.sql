DELETE FROM approval_rules WHERE doc_type = 'subcontract_order';
DELETE FROM document_approvals WHERE doc_type = 'subcontract_order';
ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_doc_type_check;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
    ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment', 'work_order', 'stock_document'));
UPDATE subcontract_orders SET status = 'open' WHERE status IN ('draft', 'pending');
ALTER TABLE subcontract_orders DROP COLUMN submitted_at, DROP COLUMN approved_by, DROP COLUMN approved_at;
ALTER TABLE subcontract_orders DROP CONSTRAINT subcontract_orders_status_check;
ALTER TABLE subcontract_orders ADD CONSTRAINT subcontract_orders_status_check CHECK (status IN ('open', 'closed', 'voided'));
