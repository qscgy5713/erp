-- 壓力測試後新增的部分索引(D57)。這些篩選條件在任何時候都只符合很小一部分資料,
-- 部分索引體積很小、寫入成本低,卻能讓首頁待審計數、未沖清帳款、未交 / 未出貨清單不必掃全表。

-- 待審 / 草稿單據:首頁「待處理單據」
CREATE INDEX sales_orders_pending_idx ON sales_orders (company_id) WHERE status = 'pending';
CREATE INDEX deliveries_pending_idx ON deliveries (company_id) WHERE status = 'pending';
CREATE INDEX settlements_pending_idx ON settlements (company_id, side) WHERE status = 'pending';
CREATE INDEX purchase_orders_pending_idx ON purchase_orders (company_id) WHERE status = 'pending';
CREATE INDEX goods_receipts_pending_idx ON goods_receipts (company_id) WHERE status = 'pending';
CREATE INDEX stock_documents_pending_idx ON stock_documents (company_id) WHERE status = 'pending';
CREATE INDEX vouchers_draft_idx ON vouchers (company_id) WHERE status = 'draft';

-- 已核准的訂單:未出貨 / 未交貨清單、可用量的保留量
CREATE INDEX sales_orders_approved_idx ON sales_orders (company_id, delivery_date) WHERE status = 'approved' AND doc_type = 'order';
CREATE INDEX purchase_orders_approved_idx ON purchase_orders (company_id, expected_date) WHERE status = 'approved';

-- 未沖清的應收 / 應付:帳款頁(只看未沖清)、帳齡、首頁、信用額度、對帳檢查
CREATE INDEX accounts_receivable_open_idx ON accounts_receivable (company_id, due_date) WHERE amount <> paid_amount;
CREATE INDEX accounts_payable_open_idx ON accounts_payable (company_id, due_date) WHERE amount <> paid_amount;
