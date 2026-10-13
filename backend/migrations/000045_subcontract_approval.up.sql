-- 委外單簽核(D105):有適用的簽核規則才需要簽核。新單金額(預計數量 × 單價)適用規則時狀態為 pending,
-- 核准後變 open;退回變 draft(可修改後再送審)。沒有適用規則的單據照舊直接 open
ALTER TABLE subcontract_orders DROP CONSTRAINT subcontract_orders_status_check;
ALTER TABLE subcontract_orders ADD CONSTRAINT subcontract_orders_status_check
    CHECK (status IN ('draft', 'pending', 'open', 'closed', 'voided'));
ALTER TABLE subcontract_orders
    ADD COLUMN submitted_at TIMESTAMPTZ,
    ADD COLUMN approved_by  BIGINT REFERENCES users (id),
    ADD COLUMN approved_at  TIMESTAMPTZ;

ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_doc_type_check;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
    ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment', 'work_order', 'stock_document',
     'subcontract_order'));
