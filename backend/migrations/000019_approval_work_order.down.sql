DELETE FROM approval_rules WHERE doc_type = 'work_order';
DELETE FROM document_approvals WHERE doc_type = 'work_order';
ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_doc_type_check;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
    ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment'));
