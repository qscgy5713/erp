-- ======== 營業稅 401 申報資料(D60) ========

-- name: VatSalesDocs :many
-- 期間內已過帳的出貨 / 銷貨退回;日期以發票日期為準,未登錄發票者以單據日期列為待處理
SELECT d.id, d.doc_no, d.doc_type, d.doc_date, COALESCE(d.invoice_date, d.doc_date)::date AS eff_date,
       d.invoice_no, d.base_untaxed, d.base_tax, t.kind AS tax_kind,
       c.code AS partner_code, c.name AS partner_name, COALESCE(c.tax_id, '')::text AS partner_tax_id
FROM deliveries d
JOIN customers c ON c.id = d.customer_id
JOIN tax_types t ON t.id = d.tax_type_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND COALESCE(d.invoice_date, d.doc_date) BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY eff_date, d.doc_no;

-- name: VatPurchaseDocs :many
-- 期間內已過帳的進貨 / 進貨退出;進貨(商品)與費用(服務)依料品類型拆分
SELECT r.id, r.doc_no, r.doc_type, r.doc_date, r.invoice_no, r.base_untaxed, r.base_tax, t.kind AS tax_kind,
       s.code AS partner_code, s.name AS partner_name, COALESCE(s.tax_id, '')::text AS partner_tax_id,
       COALESCE(SUM(l.base_amount) FILTER (WHERE i.item_type = 'goods'), 0)::numeric AS goods_amount,
       COALESCE(SUM(l.base_amount) FILTER (WHERE i.item_type <> 'goods'), 0)::numeric AS expense_amount
FROM goods_receipts r
JOIN suppliers s ON s.id = r.supplier_id
JOIN tax_types t ON t.id = r.tax_type_id
LEFT JOIN goods_receipt_lines l ON l.receipt_id = r.id
LEFT JOIN items i ON i.id = l.item_id
WHERE r.company_id = @company_id AND r.status = 'posted'
  AND r.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
GROUP BY r.id, t.kind, s.id
ORDER BY r.doc_date, r.doc_no;
