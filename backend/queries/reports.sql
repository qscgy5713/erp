-- ======== 銷售 / 採購報表(D73) ========
-- 只計已過帳的單據;金額為本位幣未稅;銷貨退回 / 進貨退出以負數計入。
-- 銷售報表依負責業務套用資料範圍(與出貨單列表相同);毛利只計「已月結成本的月份」(成本 = 基本單位數量 × 該月平均成本)。

-- name: SalesReportByCustomer :many
SELECT c.id AS customer_id, c.code AS customer_code, c.name AS customer_name,
       COUNT(DISTINCT d.id) FILTER (WHERE d.doc_type = 'delivery')::bigint AS delivery_docs,
       COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'delivery'), 0)::numeric AS delivery_amount,
       COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'return'), 0)::numeric AS return_amount,
       COALESCE(SUM(CASE WHEN ic.avg_cost IS NOT NULL THEN CASE WHEN d.doc_type = 'return' THEN -l.base_amount ELSE l.base_amount END END), 0)::numeric AS costed_amount,
       COALESCE(SUM(CASE WHEN ic.avg_cost IS NOT NULL THEN CASE WHEN d.doc_type = 'return' THEN -l.base_qty ELSE l.base_qty END * ic.avg_cost END), 0)::numeric AS cost_amount
FROM deliveries d
JOIN delivery_lines l ON l.delivery_id = d.id
JOIN customers c ON c.id = d.customer_id
LEFT JOIN users su ON su.id = d.sales_user_id
LEFT JOIN cost_closings cc ON cc.company_id = d.company_id AND cc.period = to_char(d.doc_date, 'YYYY-MM')
LEFT JOIN item_costs ic ON ic.closing_id = cc.id AND ic.item_id = l.item_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date >= @from_date AND d.doc_date <= @to_date
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
GROUP BY c.id, c.code, c.name
ORDER BY (COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'delivery'), 0) - COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'return'), 0)) DESC, c.code;

-- name: SalesReportByItem :many
SELECT i.id AS item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       COALESCE(SUM(l.base_qty) FILTER (WHERE d.doc_type = 'delivery'), 0)::numeric AS delivery_qty,
       COALESCE(SUM(l.base_qty) FILTER (WHERE d.doc_type = 'return'), 0)::numeric AS return_qty,
       COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'delivery'), 0)::numeric AS delivery_amount,
       COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'return'), 0)::numeric AS return_amount,
       COALESCE(SUM(CASE WHEN ic.avg_cost IS NOT NULL THEN CASE WHEN d.doc_type = 'return' THEN -l.base_amount ELSE l.base_amount END END), 0)::numeric AS costed_amount,
       COALESCE(SUM(CASE WHEN ic.avg_cost IS NOT NULL THEN CASE WHEN d.doc_type = 'return' THEN -l.base_qty ELSE l.base_qty END * ic.avg_cost END), 0)::numeric AS cost_amount
FROM deliveries d
JOIN delivery_lines l ON l.delivery_id = d.id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = i.base_unit_id
LEFT JOIN users su ON su.id = d.sales_user_id
LEFT JOIN cost_closings cc ON cc.company_id = d.company_id AND cc.period = to_char(d.doc_date, 'YYYY-MM')
LEFT JOIN item_costs ic ON ic.closing_id = cc.id AND ic.item_id = l.item_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date >= @from_date AND d.doc_date <= @to_date
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
GROUP BY i.id, i.code, i.name, u.name
ORDER BY (COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'delivery'), 0) - COALESCE(SUM(l.base_amount) FILTER (WHERE d.doc_type = 'return'), 0)) DESC, i.code;

-- name: SalesReportDetail :many
SELECT d.id AS doc_id, d.doc_no, d.doc_type, d.doc_date, c.code AS customer_code, c.name AS customer_name,
       i.code AS item_code, i.name AS item_name, u.name AS unit_name, l.line_no,
       CASE WHEN d.doc_type = 'return' THEN -l.base_qty ELSE l.base_qty END::numeric AS qty,
       CASE WHEN d.doc_type = 'return' THEN -l.base_amount ELSE l.base_amount END::numeric AS amount,
       (ic.avg_cost IS NOT NULL)::boolean AS has_cost,
       COALESCE(CASE WHEN d.doc_type = 'return' THEN -l.base_qty ELSE l.base_qty END * ic.avg_cost, 0)::numeric AS cost
FROM deliveries d
JOIN delivery_lines l ON l.delivery_id = d.id
JOIN customers c ON c.id = d.customer_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = i.base_unit_id
LEFT JOIN users su ON su.id = d.sales_user_id
LEFT JOIN cost_closings cc ON cc.company_id = d.company_id AND cc.period = to_char(d.doc_date, 'YYYY-MM')
LEFT JOIN item_costs ic ON ic.closing_id = cc.id AND ic.item_id = l.item_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date >= @from_date AND d.doc_date <= @to_date
  AND (sqlc.narg(customer_id)::bigint IS NULL OR d.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(item_id)::bigint IS NULL OR l.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY d.doc_date DESC, d.id DESC, l.line_no
LIMIT @lim OFFSET @off;

-- name: CountSalesReportDetail :one
SELECT count(*) FROM deliveries d
JOIN delivery_lines l ON l.delivery_id = d.id
LEFT JOIN users su ON su.id = d.sales_user_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date >= @from_date AND d.doc_date <= @to_date
  AND (sqlc.narg(customer_id)::bigint IS NULL OR d.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(item_id)::bigint IS NULL OR l.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: PurchaseReportBySupplier :many
SELECT s.id AS supplier_id, s.code AS supplier_code, s.name AS supplier_name,
       COUNT(DISTINCT r.id) FILTER (WHERE r.doc_type = 'receipt')::bigint AS receipt_docs,
       COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'receipt'), 0)::numeric AS receipt_amount,
       COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'return'), 0)::numeric AS return_amount
FROM goods_receipts r
JOIN goods_receipt_lines l ON l.receipt_id = r.id
JOIN suppliers s ON s.id = r.supplier_id
WHERE r.company_id = @company_id AND r.status = 'posted'
  AND r.doc_date >= @from_date AND r.doc_date <= @to_date
GROUP BY s.id, s.code, s.name
ORDER BY (COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'receipt'), 0) - COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'return'), 0)) DESC, s.code;

-- name: PurchaseReportByItem :many
SELECT i.id AS item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       COALESCE(SUM(l.base_qty) FILTER (WHERE r.doc_type = 'receipt'), 0)::numeric AS receipt_qty,
       COALESCE(SUM(l.base_qty) FILTER (WHERE r.doc_type = 'return'), 0)::numeric AS return_qty,
       COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'receipt'), 0)::numeric AS receipt_amount,
       COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'return'), 0)::numeric AS return_amount
FROM goods_receipts r
JOIN goods_receipt_lines l ON l.receipt_id = r.id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = i.base_unit_id
WHERE r.company_id = @company_id AND r.status = 'posted'
  AND r.doc_date >= @from_date AND r.doc_date <= @to_date
GROUP BY i.id, i.code, i.name, u.name
ORDER BY (COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'receipt'), 0) - COALESCE(SUM(l.base_amount) FILTER (WHERE r.doc_type = 'return'), 0)) DESC, i.code;

-- name: PurchaseReportDetail :many
SELECT r.id AS doc_id, r.doc_no, r.doc_type, r.doc_date, s.code AS supplier_code, s.name AS supplier_name,
       i.code AS item_code, i.name AS item_name, u.name AS unit_name, l.line_no,
       CASE WHEN r.doc_type = 'return' THEN -l.base_qty ELSE l.base_qty END::numeric AS qty,
       CASE WHEN r.doc_type = 'return' THEN -l.base_amount ELSE l.base_amount END::numeric AS amount
FROM goods_receipts r
JOIN goods_receipt_lines l ON l.receipt_id = r.id
JOIN suppliers s ON s.id = r.supplier_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = i.base_unit_id
WHERE r.company_id = @company_id AND r.status = 'posted'
  AND r.doc_date >= @from_date AND r.doc_date <= @to_date
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR r.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(item_id)::bigint IS NULL OR l.item_id = sqlc.narg(item_id))
ORDER BY r.doc_date DESC, r.id DESC, l.line_no
LIMIT @lim OFFSET @off;

-- name: CountPurchaseReportDetail :one
SELECT count(*) FROM goods_receipts r
JOIN goods_receipt_lines l ON l.receipt_id = r.id
WHERE r.company_id = @company_id AND r.status = 'posted'
  AND r.doc_date >= @from_date AND r.doc_date <= @to_date
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR r.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(item_id)::bigint IS NULL OR l.item_id = sqlc.narg(item_id));

-- name: LastPurchasePrices :many
-- 每個料品在同供應商、同幣別下,最近一張已過帳進貨單(不含退出)的單價與單位
SELECT DISTINCT ON (l.item_id) l.item_id, l.unit_id, l.unit_price, r.doc_date, r.doc_no
FROM goods_receipts r
JOIN goods_receipt_lines l ON l.receipt_id = r.id
WHERE r.company_id = @company_id AND r.supplier_id = @supplier_id AND r.currency = @currency
  AND r.status = 'posted' AND r.doc_type = 'receipt' AND l.item_id = ANY(@item_ids::bigint[])
ORDER BY l.item_id, r.doc_date DESC, r.id DESC;
