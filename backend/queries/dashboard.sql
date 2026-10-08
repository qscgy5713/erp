-- 首頁儀表板:各卡片只在使用者有對應權限時才會被呼叫;銷售相關依資料範圍(D38)

-- name: DashboardSales :one
-- 期間內已過帳出貨 − 銷貨退回的未稅本位幣金額與出貨單張數
SELECT COALESCE(SUM(CASE WHEN d.doc_type = 'delivery' THEN d.base_untaxed ELSE -d.base_untaxed END), 0)::numeric AS amount,
       count(*) FILTER (WHERE d.doc_type = 'delivery') AS delivery_count
FROM deliveries d
LEFT JOIN users su ON su.id = d.sales_user_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: DashboardSalesDaily :many
-- 近幾天每日銷售額(用於趨勢)
SELECT d.doc_date AS day,
       COALESCE(SUM(CASE WHEN d.doc_type = 'delivery' THEN d.base_untaxed ELSE -d.base_untaxed END), 0)::numeric AS amount
FROM deliveries d
LEFT JOIN users su ON su.id = d.sales_user_id
WHERE d.company_id = @company_id AND d.status = 'posted'
  AND d.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
GROUP BY d.doc_date
ORDER BY d.doc_date;

-- name: DashboardPendingSales :one
-- 待審的報價 / 訂單、出貨 / 退回、收款單(依資料範圍)
SELECT
  (SELECT count(*) FROM sales_orders x LEFT JOIN users u ON u.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending'
      AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
      AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR u.department_id = sqlc.narg(scope_dept_id))) AS sales_orders,
  (SELECT count(*) FROM deliveries x LEFT JOIN users u ON u.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending'
      AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
      AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR u.department_id = sqlc.narg(scope_dept_id))) AS deliveries,
  (SELECT count(*) FROM settlements x LEFT JOIN users u ON u.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND x.side = 'receipt'
      AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
      AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR u.department_id = sqlc.narg(scope_dept_id))) AS collections;

-- name: DashboardPendingOthers :one
SELECT
  (SELECT count(*) FROM purchase_orders x WHERE x.company_id = @company_id AND x.status = 'pending') AS purchase_orders,
  (SELECT count(*) FROM goods_receipts x WHERE x.company_id = @company_id AND x.status = 'pending') AS receipts,
  (SELECT count(*) FROM settlements x WHERE x.company_id = @company_id AND x.status = 'pending' AND x.side = 'payment') AS payments,
  (SELECT count(*) FROM stock_documents x WHERE x.company_id = @company_id AND x.status = 'pending') AS stock_documents,
  (SELECT count(*) FROM vouchers x WHERE x.company_id = @company_id AND x.status = 'draft') AS draft_vouchers;

-- name: DashboardLowStock :many
-- 低於安全庫存的商品(以所有倉庫合計判斷),缺口大的在前
SELECT i.id, i.code, i.name, i.safety_stock, u.name AS unit_name, t.total
FROM items i
JOIN units u ON u.id = i.base_unit_id
JOIN LATERAL (SELECT COALESCE(SUM(b.qty), 0)::numeric AS total FROM inventory_balances b WHERE b.item_id = i.id) t ON TRUE
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods' AND i.safety_stock > 0 AND t.total < i.safety_stock
ORDER BY (i.safety_stock - t.total) DESC, i.code
LIMIT 5;

-- name: DashboardLowStockCount :one
SELECT count(*) FROM items i
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods' AND i.safety_stock > 0
  AND COALESCE((SELECT SUM(b.qty) FROM inventory_balances b WHERE b.item_id = i.id), 0) < i.safety_stock;

-- name: DashboardReceivables :one
-- 未沖應收(本位幣,依餘額比例;退回的負數帳款一併計入,與應收帳款頁、帳齡一致)與逾期部分;依客戶負責業務的資料範圍
SELECT
  COALESCE(SUM(b.bal), 0)::numeric AS open_amount,
  COALESCE(SUM(b.bal) FILTER (WHERE r.due_date < sqlc.arg(today)::date), 0)::numeric AS overdue_amount,
  count(*) FILTER (WHERE r.due_date < sqlc.arg(today)::date) AS overdue_count
FROM accounts_receivable r
JOIN customers c ON c.id = r.customer_id
LEFT JOIN users su ON su.id = c.sales_user_id
CROSS JOIN LATERAL (SELECT (CASE WHEN r.amount = 0 THEN 0 ELSE r.base_amount * (r.amount - r.paid_amount) / r.amount END)::numeric AS bal) b
WHERE r.company_id = @company_id AND r.amount <> r.paid_amount
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: DashboardPayables :one
-- 未沖應付、已逾期、7 日內到期
SELECT
  COALESCE(SUM(b.bal), 0)::numeric AS open_amount,
  COALESCE(SUM(b.bal) FILTER (WHERE p.due_date < sqlc.arg(today)::date), 0)::numeric AS overdue_amount,
  count(*) FILTER (WHERE p.due_date < sqlc.arg(today)::date) AS overdue_count,
  COALESCE(SUM(b.bal) FILTER (WHERE p.due_date BETWEEN sqlc.arg(today)::date AND sqlc.arg(soon)::date), 0)::numeric AS due_soon_amount
FROM accounts_payable p
CROSS JOIN LATERAL (SELECT (CASE WHEN p.amount = 0 THEN 0 ELSE p.base_amount * (p.amount - p.paid_amount) / p.amount END)::numeric AS bal) b
WHERE p.company_id = @company_id AND p.amount <> p.paid_amount;

-- name: DashboardLatestClosing :one
SELECT c.period FROM cost_closings c WHERE c.company_id = @company_id ORDER BY c.period DESC LIMIT 1;

-- name: DashboardExpiryCounts :one
-- 有庫存的批號中,已過期與即將到期(today 到 until)的批號數;controlled 表示公司有效期管理的料品
SELECT
  count(*) FILTER (WHERE l.expiry_date < @today::date)::bigint AS expired,
  count(*) FILTER (WHERE l.expiry_date >= @today::date AND l.expiry_date <= @until::date)::bigint AS expiring,
  EXISTS (SELECT 1 FROM items x WHERE x.company_id = @company_id AND x.lot_control = 'lot_expiry') AS controlled
FROM (SELECT DISTINCT b.lot_id FROM inventory_lot_balances b WHERE b.company_id = @company_id AND b.qty > 0) s
JOIN item_lots l ON l.id = s.lot_id;

-- name: DashboardExpiryTop :many
-- 最早到期的前 5 個有庫存的批號(含已過期)
SELECT l.id AS lot_id, l.lot_no, l.expiry_date, i.code, i.name, SUM(b.qty)::numeric AS qty
FROM inventory_lot_balances b
JOIN item_lots l ON l.id = b.lot_id
JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id AND b.qty > 0 AND l.expiry_date IS NOT NULL AND l.expiry_date <= @until::date
GROUP BY l.id, i.code, i.name
ORDER BY l.expiry_date, i.code
LIMIT 5;
