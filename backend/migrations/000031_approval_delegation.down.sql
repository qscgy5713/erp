DELETE FROM approval_rules WHERE doc_type = 'stock_document';
DELETE FROM document_approvals WHERE doc_type = 'stock_document';
ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_doc_type_check;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
    ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment', 'work_order'));
ALTER TABLE document_approvals DROP COLUMN acting_for;
DROP TABLE approval_delegations;
