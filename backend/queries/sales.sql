-- scope_* 為資料範圍:依單據的負責業務(本人 / 同部門)過濾;未指定負責業務的單據只有「全部」範圍看得到(D38)

-- ======== 報價單 / 訂單 ========

-- name: ListSalesOrders :many
SELECT o.id, o.doc_type, o.doc_no, o.doc_date, o.valid_until, o.delivery_date, o.customer_po_no, o.status,
       o.currency, o.total_amount, o.note, o.version, o.updated_at,
       c.code AS customer_code, c.name AS customer_name, su.name AS sales_user_name, cu.name AS created_by_name,
       -- 訂單出貨狀態:依已過帳出貨量判斷(none / partial / full);報價單為 none
       (CASE
            WHEN o.doc_type <> 'order' OR NOT EXISTS (SELECT 1 FROM sales_order_lines l WHERE l.order_id = o.id) THEN 'none'
            WHEN NOT EXISTS (
                SELECT 1 FROM sales_order_lines l
                WHERE l.order_id = o.id AND l.qty > (
                    SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
                    JOIN deliveries d ON d.id = dl.delivery_id
                    WHERE dl.so_line_id = l.id AND d.status = 'posted')) THEN 'full'
            WHEN EXISTS (
                SELECT 1 FROM delivery_lines dl
                JOIN deliveries d ON d.id = dl.delivery_id
                JOIN sales_order_lines l ON l.id = dl.so_line_id
                WHERE l.order_id = o.id AND d.status = 'posted') THEN 'partial'
            ELSE 'none' END)::text AS ship_state,
       -- 報價單是否已轉訂單(未作廢)
       EXISTS (SELECT 1 FROM sales_orders so WHERE so.quotation_id = o.id AND so.status <> 'voided') AS converted
FROM sales_orders o
JOIN customers c ON c.id = o.customer_id
LEFT JOIN users su ON su.id = o.sales_user_id
LEFT JOIN users cu ON cu.id = o.created_by
WHERE o.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR o.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(customer_id)::bigint IS NULL OR o.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR o.customer_po_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR o.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR o.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR o.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY o.doc_date DESC, o.id DESC
LIMIT @lim OFFSET @off;

-- name: CountSalesOrders :one
SELECT count(*) FROM sales_orders o
LEFT JOIN users su ON su.id = o.sales_user_id
WHERE o.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR o.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(customer_id)::bigint IS NULL OR o.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR o.customer_po_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR o.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR o.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR o.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: GetSalesOrder :one
SELECT o.*, c.code AS customer_code, c.name AS customer_name, w.name AS warehouse_name,
       t.name AS tax_type_name, pt.name AS payment_term_name, q.doc_no AS quotation_no,
       su.name AS sales_user_name, su.department_id AS sales_department_id,
       cu.name AS created_by_name, sbu.name AS submitted_by_name, au.name AS approved_by_name,
       clu.name AS closed_by_name
FROM sales_orders o
JOIN customers c ON c.id = o.customer_id
JOIN warehouses w ON w.id = o.warehouse_id
JOIN tax_types t ON t.id = o.tax_type_id
LEFT JOIN payment_terms pt ON pt.id = o.payment_term_id
LEFT JOIN sales_orders q ON q.id = o.quotation_id
LEFT JOIN users su ON su.id = o.sales_user_id
LEFT JOIN users cu ON cu.id = o.created_by
LEFT JOIN users sbu ON sbu.id = o.submitted_by
LEFT JOIN users au ON au.id = o.approved_by
LEFT JOIN users clu ON clu.id = o.closed_by
WHERE o.id = @id AND o.company_id = @company_id;

-- name: LockSalesOrder :one
SELECT * FROM sales_orders WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: LockSalesOrders :many
-- 出貨過帳時鎖定來源訂單(依 id 排序,上鎖順序一致)
SELECT id FROM sales_orders
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: CreateSalesOrder :one
INSERT INTO sales_orders (company_id, doc_type, doc_no, doc_date, customer_id, sales_user_id, warehouse_id,
                          quotation_id, valid_until, delivery_date, customer_po_no, currency, exchange_rate,
                          tax_type_id, tax_rate, payment_term_id, untaxed_amount, tax_amount, total_amount, note,
                          created_by, updated_by)
VALUES (@company_id, @doc_type, @doc_no, @doc_date, @customer_id, sqlc.narg(sales_user_id), @warehouse_id,
        sqlc.narg(quotation_id), sqlc.narg(valid_until), sqlc.narg(delivery_date), @customer_po_no, @currency,
        @exchange_rate, @tax_type_id, @tax_rate, sqlc.narg(payment_term_id), @untaxed_amount, @tax_amount,
        @total_amount, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateSalesOrderHeader :one
UPDATE sales_orders
SET doc_date = @doc_date, customer_id = @customer_id, sales_user_id = sqlc.narg(sales_user_id),
    warehouse_id = @warehouse_id, quotation_id = sqlc.narg(quotation_id), valid_until = sqlc.narg(valid_until),
    delivery_date = sqlc.narg(delivery_date), customer_po_no = @customer_po_no, currency = @currency,
    exchange_rate = @exchange_rate, tax_type_id = @tax_type_id, tax_rate = @tax_rate,
    payment_term_id = sqlc.narg(payment_term_id), untaxed_amount = @untaxed_amount, tax_amount = @tax_amount,
    total_amount = @total_amount, note = @note, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetSalesOrderStatus :one
UPDATE sales_orders
SET status       = @status,
    submitted_by = CASE WHEN @action::text = 'submit' THEN @actor_id::bigint ELSE submitted_by END,
    submitted_at = CASE WHEN @action::text = 'submit' THEN now() ELSE submitted_at END,
    approved_by  = CASE WHEN @action::text = 'approve' THEN @actor_id::bigint
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_by END,
    approved_at  = CASE WHEN @action::text = 'approve' THEN now()
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_at END,
    closed_by    = CASE WHEN @action::text = 'close' THEN @actor_id::bigint
                        WHEN @action::text = 'reopen' THEN NULL ELSE closed_by END,
    closed_at    = CASE WHEN @action::text = 'close' THEN now()
                        WHEN @action::text = 'reopen' THEN NULL ELSE closed_at END,
    version      = version + 1,
    updated_by   = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListSalesOrderLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.item_type,
       u.name AS unit_name, bu.name AS base_unit_name,
       (SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
        JOIN deliveries d ON d.id = dl.delivery_id
        WHERE dl.so_line_id = l.id AND d.status = 'posted')::numeric AS delivered_qty
FROM sales_order_lines l
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
JOIN units bu ON bu.id = i.base_unit_id
WHERE l.order_id = @order_id
ORDER BY l.line_no;

-- name: DeleteSalesOrderLines :exec
DELETE FROM sales_order_lines WHERE order_id = @order_id;

-- name: AddSalesOrderLine :exec
INSERT INTO sales_order_lines (order_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount, note)
VALUES (@order_id, @line_no, @item_id, @unit_id, @qty, @factor, @base_qty, @unit_price, @amount, @note);

-- name: SalesOrderDeliveryNo :one
-- 訂單是否已被未作廢的出貨單引用(取消核准、作廢前檢查)
SELECT d.doc_no FROM delivery_lines dl
JOIN deliveries d ON d.id = dl.delivery_id
JOIN sales_order_lines l ON l.id = dl.so_line_id
WHERE l.order_id = @order_id AND d.status <> 'voided'
LIMIT 1;

-- name: QuotationOrderNo :one
-- 報價單是否已轉成未作廢的訂單(取消核准、作廢前檢查)
SELECT doc_no FROM sales_orders WHERE quotation_id = @quotation_id AND status <> 'voided' LIMIT 1;

-- name: UnshippedSalesLines :many
-- 未出貨明細:已核准訂單中尚未出齊的明細(未出貨清單、出貨單帶入用)
SELECT l.id AS so_line_id, o.id AS order_id, o.doc_no, o.doc_date, o.delivery_date, o.customer_id,
       c.code AS customer_code, c.name AS customer_name, o.currency, o.warehouse_id, o.customer_po_no,
       l.line_no, l.item_id, i.code AS item_code, i.name AS item_name, i.spec AS item_spec,
       l.unit_id, u.name AS unit_name, l.qty, l.unit_price, dlv.delivered_qty,
       (l.qty - dlv.delivered_qty)::numeric AS remaining_qty
FROM sales_order_lines l
JOIN sales_orders o ON o.id = l.order_id
JOIN customers c ON c.id = o.customer_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
LEFT JOIN users su ON su.id = o.sales_user_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(dl.qty), 0)::numeric AS delivered_qty FROM delivery_lines dl
    JOIN deliveries d ON d.id = dl.delivery_id
    WHERE dl.so_line_id = l.id AND d.status = 'posted'
) dlv
WHERE o.company_id = @company_id AND o.doc_type = 'order' AND o.status = 'approved' AND l.qty > dlv.delivered_qty
  AND (sqlc.narg(customer_id)::bigint IS NULL OR o.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(currency)::text IS NULL OR o.currency = sqlc.narg(currency))
  AND (sqlc.narg(keyword)::text IS NULL
       OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR o.customer_po_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(due_before)::date IS NULL OR o.delivery_date <= sqlc.narg(due_before))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR o.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY o.delivery_date NULLS LAST, o.doc_no, l.line_no
LIMIT @lim OFFSET @off;

-- name: CountUnshippedSalesLines :one
SELECT count(*)
FROM sales_order_lines l
JOIN sales_orders o ON o.id = l.order_id
JOIN items i ON i.id = l.item_id
LEFT JOIN users su ON su.id = o.sales_user_id
WHERE o.company_id = @company_id AND o.doc_type = 'order' AND o.status = 'approved'
  AND l.qty > (SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
               JOIN deliveries d ON d.id = dl.delivery_id
               WHERE dl.so_line_id = l.id AND d.status = 'posted')
  AND (sqlc.narg(customer_id)::bigint IS NULL OR o.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(currency)::text IS NULL OR o.currency = sqlc.narg(currency))
  AND (sqlc.narg(keyword)::text IS NULL
       OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR o.customer_po_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(due_before)::date IS NULL OR o.delivery_date <= sqlc.narg(due_before))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR o.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: ItemAvailability :many
-- 可用量 = 現有量 − 保留量(其他已核准訂單的未出貨基本單位數量)(D39)
SELECT i.id AS item_id,
       COALESCE((SELECT b.qty FROM inventory_balances b
                 WHERE b.item_id = i.id AND b.warehouse_id = @warehouse_id::bigint), 0)::numeric AS on_hand,
       COALESCE((SELECT SUM(GREATEST(l.qty - (
                     SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
                     JOIN deliveries d ON d.id = dl.delivery_id
                     WHERE dl.so_line_id = l.id AND d.status = 'posted'), 0) * l.factor)
                 FROM sales_order_lines l
                 JOIN sales_orders o ON o.id = l.order_id
                 WHERE l.item_id = i.id AND o.warehouse_id = @warehouse_id::bigint AND o.doc_type = 'order'
                   AND o.status = 'approved' AND o.id <> @exclude_order_id::bigint), 0)::numeric AS reserved
FROM items i
WHERE i.company_id = @company_id AND i.id = ANY(@item_ids::bigint[]) AND i.item_type = 'goods';

-- name: CustomerExposure :one
-- 信用額度占用(本位幣含稅):未沖應收 + 其他已核准訂單的未出貨金額(D39)
SELECT
    COALESCE((SELECT SUM(CASE WHEN ar.amount = 0 THEN 0
                              ELSE ar.base_amount * (ar.amount - ar.paid_amount) / ar.amount END)
              FROM accounts_receivable ar WHERE ar.customer_id = @customer_id), 0)::numeric AS ar_open,
    COALESCE((SELECT SUM(GREATEST(l.qty - (
                  SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
                  JOIN deliveries d ON d.id = dl.delivery_id
                  WHERE dl.so_line_id = l.id AND d.status = 'posted'), 0)
                  * l.unit_price * (1 + o.tax_rate) * o.exchange_rate)
              FROM sales_order_lines l
              JOIN sales_orders o ON o.id = l.order_id
              WHERE o.customer_id = @customer_id AND o.doc_type = 'order' AND o.status = 'approved'
                AND o.id <> @exclude_order_id::bigint), 0)::numeric AS open_orders;

-- ======== 出貨單 / 銷貨退回單 ========

-- name: ListDeliveries :many
SELECT d.id, d.doc_type, d.doc_no, d.doc_date, d.status, d.currency, d.total_amount, d.invoice_no, d.note,
       d.version, d.updated_at, c.code AS customer_code, c.name AS customer_name, w.name AS warehouse_name,
       su.name AS sales_user_name, cu.name AS created_by_name
FROM deliveries d
JOIN customers c ON c.id = d.customer_id
JOIN warehouses w ON w.id = d.warehouse_id
LEFT JOIN users su ON su.id = d.sales_user_id
LEFT JOIN users cu ON cu.id = d.created_by
WHERE d.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR d.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(customer_id)::bigint IS NULL OR d.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR d.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR d.invoice_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR d.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR d.doc_date <= sqlc.narg(to_date))
  AND (NOT @no_invoice::boolean OR d.invoice_no = '')
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY d.doc_date DESC, d.id DESC
LIMIT @lim OFFSET @off;

-- name: CountDeliveries :one
SELECT count(*) FROM deliveries d
LEFT JOIN users su ON su.id = d.sales_user_id
WHERE d.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR d.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(customer_id)::bigint IS NULL OR d.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR d.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR d.invoice_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR d.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR d.doc_date <= sqlc.narg(to_date))
  AND (NOT @no_invoice::boolean OR d.invoice_no = '')
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR d.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: GetDelivery :one
SELECT d.*, c.code AS customer_code, c.name AS customer_name, w.name AS warehouse_name,
       t.name AS tax_type_name, pt.name AS payment_term_name,
       su.name AS sales_user_name, su.department_id AS sales_department_id,
       cu.name AS created_by_name, sbu.name AS submitted_by_name, au.name AS approved_by_name,
       pu.name AS posted_by_name
FROM deliveries d
JOIN customers c ON c.id = d.customer_id
JOIN warehouses w ON w.id = d.warehouse_id
JOIN tax_types t ON t.id = d.tax_type_id
LEFT JOIN payment_terms pt ON pt.id = d.payment_term_id
LEFT JOIN users su ON su.id = d.sales_user_id
LEFT JOIN users cu ON cu.id = d.created_by
LEFT JOIN users sbu ON sbu.id = d.submitted_by
LEFT JOIN users au ON au.id = d.approved_by
LEFT JOIN users pu ON pu.id = d.posted_by
WHERE d.id = @id AND d.company_id = @company_id;

-- name: LockDelivery :one
SELECT * FROM deliveries WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: LockDeliveries :many
-- 退回過帳時鎖定來源出貨單(依 id 排序)
SELECT id FROM deliveries
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: CreateDelivery :one
INSERT INTO deliveries (company_id, doc_type, doc_no, doc_date, customer_id, sales_user_id, warehouse_id, currency,
                        exchange_rate, tax_type_id, tax_rate, payment_term_id, untaxed_amount, tax_amount,
                        total_amount, base_untaxed, base_tax, base_total, note, created_by, updated_by)
VALUES (@company_id, @doc_type, @doc_no, @doc_date, @customer_id, sqlc.narg(sales_user_id), @warehouse_id, @currency,
        @exchange_rate, @tax_type_id, @tax_rate, sqlc.narg(payment_term_id), @untaxed_amount, @tax_amount,
        @total_amount, @base_untaxed, @base_tax, @base_total, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateDeliveryHeader :one
UPDATE deliveries
SET doc_date = @doc_date, customer_id = @customer_id, sales_user_id = sqlc.narg(sales_user_id),
    warehouse_id = @warehouse_id, currency = @currency, exchange_rate = @exchange_rate, tax_type_id = @tax_type_id,
    tax_rate = @tax_rate, payment_term_id = sqlc.narg(payment_term_id), untaxed_amount = @untaxed_amount,
    tax_amount = @tax_amount, total_amount = @total_amount, base_untaxed = @base_untaxed, base_tax = @base_tax,
    base_total = @base_total, note = @note, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetDeliveryStatus :one
UPDATE deliveries
SET status       = @status,
    submitted_by = CASE WHEN @action::text = 'submit' THEN @actor_id::bigint ELSE submitted_by END,
    submitted_at = CASE WHEN @action::text = 'submit' THEN now() ELSE submitted_at END,
    approved_by  = CASE WHEN @action::text = 'approve' THEN @actor_id::bigint
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_by END,
    approved_at  = CASE WHEN @action::text = 'approve' THEN now()
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_at END,
    posted_by    = CASE WHEN @action::text = 'post' THEN @actor_id::bigint
                        WHEN @action::text = 'unpost' THEN NULL ELSE posted_by END,
    posted_at    = CASE WHEN @action::text = 'post' THEN now()
                        WHEN @action::text = 'unpost' THEN NULL ELSE posted_at END,
    version      = version + 1,
    updated_by   = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: SetDeliveryInvoice :one
UPDATE deliveries
SET invoice_no = @invoice_no, invoice_date = sqlc.narg(invoice_date), version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status <> 'voided'
RETURNING *;

-- name: ListDeliveryLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.item_type,
       u.name AS unit_name, bu.name AS base_unit_name,
       so.doc_no AS so_no, src.doc_no AS source_delivery_no
FROM delivery_lines l
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
JOIN units bu ON bu.id = i.base_unit_id
LEFT JOIN sales_order_lines sol ON sol.id = l.so_line_id
LEFT JOIN sales_orders so ON so.id = sol.order_id
LEFT JOIN delivery_lines sdl ON sdl.id = l.delivery_line_id
LEFT JOIN deliveries src ON src.id = sdl.delivery_id
WHERE l.delivery_id = @delivery_id
ORDER BY l.line_no;

-- name: DeleteDeliveryLines :exec
DELETE FROM delivery_lines WHERE delivery_id = @delivery_id;

-- name: AddDeliveryLine :exec
INSERT INTO delivery_lines (delivery_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount,
                            base_amount, so_line_id, delivery_line_id, note)
VALUES (@delivery_id, @line_no, @item_id, @unit_id, @qty, @factor, @base_qty, @unit_price, @amount, @base_amount,
        sqlc.narg(so_line_id), sqlc.narg(delivery_line_id), @note);

-- name: SoLineRefs :many
-- 出貨明細引用的訂單明細,以及「其他」已過帳出貨單的已出貨量
SELECT l.id, l.order_id, o.doc_no, o.doc_type, o.status, o.customer_id, o.currency, l.item_id, l.unit_id, l.qty,
       (SELECT COALESCE(SUM(dl.qty), 0) FROM delivery_lines dl
        JOIN deliveries d ON d.id = dl.delivery_id
        WHERE dl.so_line_id = l.id AND d.status = 'posted' AND d.id <> @exclude_delivery_id::bigint)::numeric
           AS delivered_qty
FROM sales_order_lines l
JOIN sales_orders o ON o.id = l.order_id
WHERE o.company_id = @company_id AND l.id = ANY(@ids::bigint[]);

-- name: DeliveryLineRefs :many
-- 退回明細引用的出貨明細,以及「其他」已過帳退回單的已退量
SELECT l.id, l.delivery_id, d.doc_no, d.doc_type, d.status, d.customer_id, d.currency, l.item_id, l.unit_id, l.qty,
       (SELECT COALESCE(SUM(xl.qty), 0) FROM delivery_lines xl
        JOIN deliveries x ON x.id = xl.delivery_id
        WHERE xl.delivery_line_id = l.id AND x.status = 'posted' AND x.id <> @exclude_delivery_id::bigint)::numeric
           AS returned_qty
FROM delivery_lines l
JOIN deliveries d ON d.id = l.delivery_id
WHERE d.company_id = @company_id AND l.id = ANY(@ids::bigint[]);

-- name: DeliveryReturnNo :one
-- 出貨單是否已被未作廢的退回單引用(反過帳前檢查)
SELECT x.doc_no FROM delivery_lines xl
JOIN deliveries x ON x.id = xl.delivery_id
JOIN delivery_lines l ON l.id = xl.delivery_line_id
WHERE l.delivery_id = @delivery_id AND x.status <> 'voided'
LIMIT 1;

-- name: ReturnableDeliveryLines :many
-- 可退回的出貨明細:已過帳出貨單中尚未退完的明細(退回單帶入用)
SELECT l.id AS delivery_line_id, d.id AS delivery_id, d.doc_no, d.doc_date, d.warehouse_id, l.line_no,
       l.item_id, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, l.unit_id, u.name AS unit_name,
       l.qty, l.unit_price, ret.returned_qty, (l.qty - ret.returned_qty)::numeric AS remaining_qty
FROM delivery_lines l
JOIN deliveries d ON d.id = l.delivery_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(xl.qty), 0)::numeric AS returned_qty FROM delivery_lines xl
    JOIN deliveries x ON x.id = xl.delivery_id
    WHERE xl.delivery_line_id = l.id AND x.status = 'posted'
) ret
WHERE d.company_id = @company_id AND d.doc_type = 'delivery' AND d.status = 'posted'
  AND d.customer_id = @customer_id AND d.currency = @currency AND l.qty > ret.returned_qty
  AND (sqlc.narg(keyword)::text IS NULL
       OR d.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY d.doc_date DESC, d.doc_no DESC, l.line_no
LIMIT 200;

-- ======== 客戶 ========

-- name: CustomerForDoc :one
SELECT c.id, c.code, c.name, c.is_active, c.sales_user_id, c.credit_limit, su.department_id AS sales_department_id
FROM customers c
LEFT JOIN users su ON su.id = c.sales_user_id
WHERE c.id = @id AND c.company_id = @company_id;

-- name: CustomerOptions :many
-- 開單選客戶:只需登入,依資料範圍過濾,只回傳啟用中的精簡欄位
SELECT c.id, c.code, c.name, c.short_name, c.currency, c.tax_type_id, c.payment_term_id, c.sales_user_id,
       su.name AS sales_user_name
FROM customers c
LEFT JOIN users su ON su.id = c.sales_user_id
WHERE c.company_id = @company_id AND c.is_active
  AND (sqlc.narg(keyword)::text IS NULL
       OR c.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.short_name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY c.code
LIMIT 20;

-- ======== 應收帳款 ========

-- name: InsertReceivable :exec
INSERT INTO accounts_receivable (company_id, customer_id, source_type, source_id, source_no, doc_date, due_date,
                                 currency, exchange_rate, amount, base_amount, created_by)
VALUES (@company_id, @customer_id, @source_type, @source_id, @source_no, @doc_date, @due_date, @currency,
        @exchange_rate, @amount, @base_amount, sqlc.narg(created_by));

-- name: LockReceivableBySource :one
SELECT * FROM accounts_receivable WHERE source_type = @source_type AND source_id = @source_id FOR UPDATE;

-- name: DeleteReceivable :exec
DELETE FROM accounts_receivable WHERE id = @id;

-- name: ListReceivables :many
-- 資料範圍依客戶目前的負責業務
SELECT r.*, c.code AS customer_code, c.name AS customer_name
FROM accounts_receivable r
JOIN customers c ON c.id = r.customer_id
LEFT JOIN users su ON su.id = c.sales_user_id
WHERE r.company_id = @company_id
  AND (sqlc.narg(customer_id)::bigint IS NULL OR r.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR r.source_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR r.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR r.doc_date <= sqlc.narg(to_date))
  AND (NOT @open_only::boolean OR r.amount <> r.paid_amount)
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY r.due_date, r.id
LIMIT @lim OFFSET @off;

-- name: CountReceivables :one
SELECT count(*) AS total, COALESCE(SUM(r.base_amount), 0)::numeric AS base_amount_sum
FROM accounts_receivable r
JOIN customers c ON c.id = r.customer_id
LEFT JOIN users su ON su.id = c.sales_user_id
WHERE r.company_id = @company_id
  AND (sqlc.narg(customer_id)::bigint IS NULL OR r.customer_id = sqlc.narg(customer_id))
  AND (sqlc.narg(keyword)::text IS NULL OR r.source_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR r.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR r.doc_date <= sqlc.narg(to_date))
  AND (NOT @open_only::boolean OR r.amount <> r.paid_amount)
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: LockCustomer :exec
-- 核准訂單檢查信用額度時鎖定客戶,同一客戶的核准依序進行
SELECT id FROM customers WHERE id = @id AND company_id = @company_id FOR UPDATE;
