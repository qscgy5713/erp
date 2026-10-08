-- 壓力測試造資料:約一年規模的中小企業資料量,灌在獨立資料庫(erp_perf),不碰開發資料。
-- 前提:已套用 migration、已建立 admin(cmd/cli create-admin)。可重複執行前請先重建資料庫。
-- 規模:3,000 料品 / 800 客戶 / 300 供應商 / 5 倉庫;銷售訂單 30,000(90,000 明細)、出貨單 25,000(75,000 明細)與應收、
--       採購單 12,000、進貨單 10,000 與應付;庫存流水 156,000;傳票 50,000(100,000 分錄)。
SELECT setseed(0.42);
\timing on

CREATE TEMP TABLE params AS SELECT 1::bigint AS company_id,
    (SELECT id FROM units WHERE code = 'PCS' LIMIT 1) AS unit_id,
    (SELECT id FROM tax_types WHERE code = 'TX5' LIMIT 1) AS tax_id,
    (SELECT id FROM payment_terms WHERE code = 'M30' LIMIT 1) AS term_id;

-- ---- 主檔 ----
INSERT INTO warehouses (company_id, code, name) SELECT 1, 'W' || g, '倉庫' || g FROM generate_series(1, 5) g;
INSERT INTO items (company_id, code, name, spec, base_unit_id, tax_type_id, safety_stock, list_price)
SELECT 1, 'ITEM' || lpad(g::text, 5, '0'), '測試料品' || g, '規格' || (g % 17), p.unit_id, p.tax_id,
       CASE WHEN g % 10 = 0 THEN 500 ELSE 0 END, (10 + g % 500)
FROM generate_series(1, 3000) g, params p;
INSERT INTO customers (company_id, code, name, short_name, currency, tax_type_id, payment_term_id, credit_limit)
SELECT 1, 'C' || lpad(g::text, 5, '0'), '測試客戶' || g || '股份有限公司', '客戶' || g, 'TWD', p.tax_id, p.term_id, 0
FROM generate_series(1, 800) g, params p;
INSERT INTO suppliers (company_id, code, name, short_name, currency, tax_type_id, payment_term_id)
SELECT 1, 'S' || lpad(g::text, 5, '0'), '測試供應商' || g || '有限公司', '供應商' || g, 'TWD', p.tax_id, p.term_id
FROM generate_series(1, 300) g, params p;

CREATE TEMP TABLE item_ids AS SELECT row_number() OVER (ORDER BY id) AS rn, id FROM items WHERE code LIKE 'ITEM%';
CREATE TEMP TABLE cust_ids AS SELECT row_number() OVER (ORDER BY id) AS rn, id FROM customers WHERE code LIKE 'C0%';
CREATE TEMP TABLE sup_ids AS SELECT row_number() OVER (ORDER BY id) AS rn, id FROM suppliers WHERE code LIKE 'S0%';
CREATE TEMP TABLE wh_ids AS SELECT row_number() OVER (ORDER BY id) AS rn, id FROM warehouses WHERE code LIKE 'W%';

-- ---- 庫存:期初 + 進出流水,現有量由流水加總 ----
INSERT INTO inventory_transactions (company_id, item_id, warehouse_id, doc_date, qty, unit_cost, source_type, source_id, source_no)
SELECT 1, i.id, w.id, DATE '2025-12-31', 2000, 50 + (i.rn % 100), 'opening_stock', 1, 'OPEN-STK-1'
FROM item_ids i, wh_ids w WHERE w.rn <= 2;
INSERT INTO inventory_transactions (company_id, item_id, warehouse_id, doc_date, qty, unit_cost, source_type, source_id, source_no)
SELECT 1, i.id, w.id, DATE '2026-01-01' + (random() * 270)::int,
       CASE WHEN g % 3 = 0 THEN (1 + random() * 20)::int ELSE -(1 + random() * 5)::int END,
       50 + (i.rn % 100), CASE WHEN g % 3 = 0 THEN 'goods_receipt' ELSE 'delivery' END, 100000 + g, 'PERF' || g
FROM generate_series(1, 150000) g
JOIN item_ids i ON i.rn = 1 + (g * 7919) % 3000
JOIN wh_ids w ON w.rn = 1 + (g % 2);
INSERT INTO inventory_balances (company_id, item_id, warehouse_id, qty)
SELECT 1, item_id, warehouse_id, SUM(qty) FROM inventory_transactions GROUP BY item_id, warehouse_id;

-- ---- 銷售:訂單、出貨、應收 ----
INSERT INTO sales_orders (company_id, doc_type, doc_no, doc_date, customer_id, warehouse_id, currency, exchange_rate,
                          tax_type_id, tax_rate, untaxed_amount, tax_amount, total_amount, status, delivery_date)
SELECT 1, 'order', 'PSO' || lpad(g::text, 7, '0'), DATE '2026-01-01' + (g % 280), c.id, w.id, 'TWD', 1, p.tax_id, 0.05,
       3000, 150, 3150, (ARRAY['approved', 'closed', 'closed', 'draft', 'pending'])[1 + g % 5], DATE '2026-01-01' + (g % 280) + 7
FROM generate_series(1, 30000) g CROSS JOIN params p
JOIN cust_ids c ON c.rn = 1 + (g * 31) % 800 JOIN wh_ids w ON w.rn = 1 + g % 5;
INSERT INTO sales_order_lines (order_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount)
SELECT o.id, l, i.id, p.unit_id, 10, 1, 10, 100, 1000
FROM sales_orders o CROSS JOIN generate_series(1, 3) l CROSS JOIN params p
JOIN item_ids i ON i.rn = 1 + (o.id * 13 + l * 101) % 3000 WHERE o.doc_no LIKE 'PSO%';

INSERT INTO deliveries (company_id, doc_type, doc_no, doc_date, customer_id, warehouse_id, currency, exchange_rate, tax_type_id,
                        tax_rate, untaxed_amount, tax_amount, total_amount, base_untaxed, base_tax, base_total, status, invoice_no)
SELECT 1, 'delivery', 'PDN' || lpad(g::text, 7, '0'), DATE '2026-01-01' + (g % 280), c.id, w.id, 'TWD', 1, p.tax_id, 0.05,
       3000, 150, 3150, 3000, 150, 3150, (ARRAY['posted', 'posted', 'posted', 'draft', 'pending'])[1 + g % 5],
       CASE WHEN g % 2 = 0 THEN 'AB' || lpad(g::text, 8, '0') ELSE '' END
FROM generate_series(1, 25000) g CROSS JOIN params p
JOIN cust_ids c ON c.rn = 1 + (g * 37) % 800 JOIN wh_ids w ON w.rn = 1 + g % 5;
INSERT INTO delivery_lines (delivery_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount, base_amount)
SELECT d.id, l, i.id, p.unit_id, 10, 1, 10, 100, 1000, 1000
FROM deliveries d CROSS JOIN generate_series(1, 3) l CROSS JOIN params p
JOIN item_ids i ON i.rn = 1 + (d.id * 17 + l * 103) % 3000 WHERE d.doc_no LIKE 'PDN%';
INSERT INTO accounts_receivable (company_id, customer_id, source_type, source_id, source_no, doc_date, due_date, currency,
                                 exchange_rate, amount, base_amount, paid_amount)
SELECT 1, d.customer_id, 'delivery', d.id, d.doc_no, d.doc_date, d.doc_date + 30, 'TWD', 1, 3150, 3150,
       CASE WHEN d.id % 5 < 3 THEN 3150 WHEN d.id % 5 = 3 THEN 1000 ELSE 0 END
FROM deliveries d WHERE d.status = 'posted' AND d.doc_no LIKE 'PDN%';

-- ---- 採購:採購單、進貨、應付 ----
INSERT INTO purchase_orders (company_id, doc_no, doc_date, supplier_id, warehouse_id, currency, exchange_rate, tax_type_id,
                             tax_rate, untaxed_amount, tax_amount, total_amount, status, expected_date)
SELECT 1, 'PPO' || lpad(g::text, 7, '0'), DATE '2026-01-01' + (g % 280), s.id, w.id, 'TWD', 1, p.tax_id, 0.05, 2000, 100, 2100,
       (ARRAY['approved', 'closed', 'closed', 'draft', 'pending'])[1 + g % 5], DATE '2026-01-01' + (g % 280) + 14
FROM generate_series(1, 12000) g CROSS JOIN params p
JOIN sup_ids s ON s.rn = 1 + (g * 29) % 300 JOIN wh_ids w ON w.rn = 1 + g % 5;
INSERT INTO purchase_order_lines (order_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount)
SELECT o.id, l, i.id, p.unit_id, 20, 1, 20, 50, 1000
FROM purchase_orders o CROSS JOIN generate_series(1, 2) l CROSS JOIN params p
JOIN item_ids i ON i.rn = 1 + (o.id * 19 + l * 107) % 3000 WHERE o.doc_no LIKE 'PPO%';
INSERT INTO goods_receipts (company_id, doc_type, doc_no, doc_date, supplier_id, warehouse_id, currency, exchange_rate, tax_type_id,
                            tax_rate, untaxed_amount, tax_amount, total_amount, base_untaxed, base_tax, base_total, status)
SELECT 1, 'receipt', 'PGR' || lpad(g::text, 7, '0'), DATE '2026-01-01' + (g % 280), s.id, w.id, 'TWD', 1, p.tax_id, 0.05,
       2000, 100, 2100, 2000, 100, 2100, (ARRAY['posted', 'posted', 'posted', 'draft', 'pending'])[1 + g % 5]
FROM generate_series(1, 10000) g CROSS JOIN params p
JOIN sup_ids s ON s.rn = 1 + (g * 41) % 300 JOIN wh_ids w ON w.rn = 1 + g % 5;
INSERT INTO goods_receipt_lines (receipt_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount, base_amount)
SELECT r.id, l, i.id, p.unit_id, 20, 1, 20, 50, 1000, 1000
FROM goods_receipts r CROSS JOIN generate_series(1, 2) l CROSS JOIN params p
JOIN item_ids i ON i.rn = 1 + (r.id * 23 + l * 109) % 3000 WHERE r.doc_no LIKE 'PGR%';
INSERT INTO accounts_payable (company_id, supplier_id, source_type, source_id, source_no, doc_date, due_date, currency,
                              exchange_rate, amount, base_amount, paid_amount)
SELECT 1, r.supplier_id, 'goods_receipt', r.id, r.doc_no, r.doc_date, r.doc_date + 30, 'TWD', 1, 2100, 2100,
       CASE WHEN r.id % 4 < 2 THEN 2100 ELSE 0 END
FROM goods_receipts r WHERE r.status = 'posted' AND r.doc_no LIKE 'PGR%';

-- ---- 會計:已過帳傳票(每張兩個分錄,借貸平衡) ----
CREATE TEMP TABLE acct_ids AS
    SELECT row_number() OVER (ORDER BY id) AS rn, id FROM accounts WHERE is_postable AND code IN
    ('1101', '1102', '1131', '1141', '2101', '2111', '4101', '5101', '6101', '6102', '6103', '6104', '6105');
INSERT INTO vouchers (company_id, doc_no, voucher_date, source_type, description, status, total_amount, posted_at)
SELECT 1, 'PJV' || lpad(g::text, 7, '0'), DATE '2026-01-01' + (g % 280), 'manual', '壓測傳票' || g, 'posted', 1000 + g % 9000, now()
FROM generate_series(1, 50000) g;
INSERT INTO voucher_lines (voucher_id, line_no, account_id, debit, credit)
SELECT v.id, l, a.id, CASE WHEN l = 1 THEN v.total_amount ELSE 0 END, CASE WHEN l = 2 THEN v.total_amount ELSE 0 END
FROM vouchers v CROSS JOIN generate_series(1, 2) l
JOIN acct_ids a ON a.rn = 1 + (v.id * 3 + l * 5) % 13 WHERE v.doc_no LIKE 'PJV%';

ANALYZE;
SELECT 'items' AS t, count(*) FROM items UNION ALL SELECT 'sales_orders', count(*) FROM sales_orders
UNION ALL SELECT 'deliveries', count(*) FROM deliveries UNION ALL SELECT 'accounts_receivable', count(*) FROM accounts_receivable
UNION ALL SELECT 'inventory_transactions', count(*) FROM inventory_transactions UNION ALL SELECT 'vouchers', count(*) FROM vouchers
UNION ALL SELECT 'voucher_lines', count(*) FROM voucher_lines;
