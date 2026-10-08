-- ======== 採購單 ========

-- name: ListPurchaseOrders :many
SELECT o.id, o.doc_no, o.doc_date, o.expected_date, o.status, o.currency, o.total_amount, o.note,
       o.version, o.updated_at, s.code AS supplier_code, s.name AS supplier_name, cu.name AS created_by_name,
       -- 交貨狀態:依已過帳進貨量判斷(none 未交 / partial 部分 / full 交齊)
       (CASE
            WHEN NOT EXISTS (SELECT 1 FROM purchase_order_lines l WHERE l.order_id = o.id) THEN 'none'
            WHEN NOT EXISTS (
                SELECT 1 FROM purchase_order_lines l
                WHERE l.order_id = o.id AND l.qty > (
                    SELECT COALESCE(SUM(rl.qty), 0) FROM goods_receipt_lines rl
                    JOIN goods_receipts r ON r.id = rl.receipt_id
                    WHERE rl.po_line_id = l.id AND r.status = 'posted')) THEN 'full'
            WHEN EXISTS (
                SELECT 1 FROM goods_receipt_lines rl
                JOIN goods_receipts r ON r.id = rl.receipt_id
                JOIN purchase_order_lines l ON l.id = rl.po_line_id
                WHERE l.order_id = o.id AND r.status = 'posted') THEN 'partial'
            ELSE 'none' END)::text AS receipt_state
FROM purchase_orders o
JOIN suppliers s ON s.id = o.supplier_id
LEFT JOIN users cu ON cu.id = o.created_by
WHERE o.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR o.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR o.doc_date <= sqlc.narg(to_date))
ORDER BY o.doc_date DESC, o.id DESC
LIMIT @lim OFFSET @off;

-- name: CountPurchaseOrders :one
SELECT count(*) FROM purchase_orders o
WHERE o.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR o.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR o.doc_date <= sqlc.narg(to_date));

-- name: GetPurchaseOrder :one
SELECT o.*, s.code AS supplier_code, s.name AS supplier_name, w.name AS warehouse_name,
       t.name AS tax_type_name, pt.name AS payment_term_name,
       cu.name AS created_by_name, su.name AS submitted_by_name, au.name AS approved_by_name,
       clu.name AS closed_by_name
FROM purchase_orders o
JOIN suppliers s ON s.id = o.supplier_id
JOIN warehouses w ON w.id = o.warehouse_id
JOIN tax_types t ON t.id = o.tax_type_id
LEFT JOIN payment_terms pt ON pt.id = o.payment_term_id
LEFT JOIN users cu ON cu.id = o.created_by
LEFT JOIN users su ON su.id = o.submitted_by
LEFT JOIN users au ON au.id = o.approved_by
LEFT JOIN users clu ON clu.id = o.closed_by
WHERE o.id = @id AND o.company_id = @company_id;

-- name: LockPurchaseOrder :one
SELECT * FROM purchase_orders WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: LockPurchaseOrders :many
-- 進貨過帳時鎖定來源採購單(依 id 排序,上鎖順序一致)
SELECT id, doc_no, status FROM purchase_orders
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: CreatePurchaseOrder :one
INSERT INTO purchase_orders (company_id, doc_no, doc_date, supplier_id, warehouse_id, expected_date, currency,
                             exchange_rate, tax_type_id, tax_rate, payment_term_id, untaxed_amount, tax_amount,
                             total_amount, note, created_by, updated_by)
VALUES (@company_id, @doc_no, @doc_date, @supplier_id, @warehouse_id, sqlc.narg(expected_date), @currency,
        @exchange_rate, @tax_type_id, @tax_rate, sqlc.narg(payment_term_id), @untaxed_amount, @tax_amount,
        @total_amount, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdatePurchaseOrderHeader :one
UPDATE purchase_orders
SET doc_date = @doc_date, supplier_id = @supplier_id, warehouse_id = @warehouse_id,
    expected_date = sqlc.narg(expected_date), currency = @currency, exchange_rate = @exchange_rate,
    tax_type_id = @tax_type_id, tax_rate = @tax_rate, payment_term_id = sqlc.narg(payment_term_id),
    untaxed_amount = @untaxed_amount, tax_amount = @tax_amount, total_amount = @total_amount, note = @note,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetPurchaseOrderStatus :one
UPDATE purchase_orders
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

-- name: ListPurchaseOrderLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.item_type,
       u.name AS unit_name, bu.name AS base_unit_name,
       (SELECT COALESCE(SUM(rl.qty), 0) FROM goods_receipt_lines rl
        JOIN goods_receipts r ON r.id = rl.receipt_id
        WHERE rl.po_line_id = l.id AND r.status = 'posted')::numeric AS received_qty
FROM purchase_order_lines l
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
JOIN units bu ON bu.id = i.base_unit_id
WHERE l.order_id = @order_id
ORDER BY l.line_no;

-- name: DeletePurchaseOrderLines :exec
DELETE FROM purchase_order_lines WHERE order_id = @order_id;

-- name: AddPurchaseOrderLine :exec
INSERT INTO purchase_order_lines (order_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount, note)
VALUES (@order_id, @line_no, @item_id, @unit_id, @qty, @factor, @base_qty, @unit_price, @amount, @note);

-- name: PurchaseOrderReceiptNo :one
-- 採購單是否已被未作廢的進貨單引用(取消核准、作廢前檢查)
SELECT r.doc_no FROM goods_receipt_lines rl
JOIN goods_receipts r ON r.id = rl.receipt_id
JOIN purchase_order_lines l ON l.id = rl.po_line_id
WHERE l.order_id = @order_id AND r.status <> 'voided'
LIMIT 1;

-- name: OutstandingPurchaseLines :many
-- 未交貨明細:已核准採購單中尚未交齊的明細(未交貨清單、進貨單帶入用)
SELECT l.id AS po_line_id, o.id AS order_id, o.doc_no, o.doc_date, o.expected_date, o.supplier_id,
       s.code AS supplier_code, s.name AS supplier_name, o.currency, o.warehouse_id,
       l.line_no, l.item_id, i.code AS item_code, i.name AS item_name, i.spec AS item_spec,
       l.unit_id, u.name AS unit_name, l.qty, l.unit_price, rec.received_qty,
       (l.qty - rec.received_qty)::numeric AS remaining_qty
FROM purchase_order_lines l
JOIN purchase_orders o ON o.id = l.order_id
JOIN suppliers s ON s.id = o.supplier_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(rl.qty), 0)::numeric AS received_qty FROM goods_receipt_lines rl
    JOIN goods_receipts r ON r.id = rl.receipt_id
    WHERE rl.po_line_id = l.id AND r.status = 'posted'
) rec
WHERE o.company_id = @company_id AND o.status = 'approved' AND l.qty > rec.received_qty
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(currency)::text IS NULL OR o.currency = sqlc.narg(currency))
  AND (sqlc.narg(keyword)::text IS NULL
       OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(due_before)::date IS NULL OR o.expected_date <= sqlc.narg(due_before))
ORDER BY o.expected_date NULLS LAST, o.doc_no, l.line_no
LIMIT @lim OFFSET @off;

-- name: CountOutstandingPurchaseLines :one
SELECT count(*)
FROM purchase_order_lines l
JOIN purchase_orders o ON o.id = l.order_id
JOIN items i ON i.id = l.item_id
WHERE o.company_id = @company_id AND o.status = 'approved'
  AND l.qty > (SELECT COALESCE(SUM(rl.qty), 0) FROM goods_receipt_lines rl
               JOIN goods_receipts r ON r.id = rl.receipt_id
               WHERE rl.po_line_id = l.id AND r.status = 'posted')
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(currency)::text IS NULL OR o.currency = sqlc.narg(currency))
  AND (sqlc.narg(keyword)::text IS NULL
       OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(due_before)::date IS NULL OR o.expected_date <= sqlc.narg(due_before));

-- ======== 進貨單 / 進貨退出單 ========

-- name: ListGoodsReceipts :many
SELECT r.id, r.doc_type, r.doc_no, r.doc_date, r.status, r.currency, r.total_amount, r.invoice_no, r.note,
       r.version, r.updated_at, s.code AS supplier_code, s.name AS supplier_name, w.name AS warehouse_name,
       cu.name AS created_by_name
FROM goods_receipts r
JOIN suppliers s ON s.id = r.supplier_id
JOIN warehouses w ON w.id = r.warehouse_id
LEFT JOIN users cu ON cu.id = r.created_by
WHERE r.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR r.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR r.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR r.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR r.invoice_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR r.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR r.doc_date <= sqlc.narg(to_date))
ORDER BY r.doc_date DESC, r.id DESC
LIMIT @lim OFFSET @off;

-- name: CountGoodsReceipts :one
SELECT count(*) FROM goods_receipts r
WHERE r.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR r.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR r.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR r.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR r.invoice_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR r.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR r.doc_date <= sqlc.narg(to_date));

-- name: GetGoodsReceipt :one
SELECT r.*, s.code AS supplier_code, s.name AS supplier_name, w.name AS warehouse_name,
       t.name AS tax_type_name, pt.name AS payment_term_name,
       cu.name AS created_by_name, su.name AS submitted_by_name, au.name AS approved_by_name,
       pu.name AS posted_by_name
FROM goods_receipts r
JOIN suppliers s ON s.id = r.supplier_id
JOIN warehouses w ON w.id = r.warehouse_id
JOIN tax_types t ON t.id = r.tax_type_id
LEFT JOIN payment_terms pt ON pt.id = r.payment_term_id
LEFT JOIN users cu ON cu.id = r.created_by
LEFT JOIN users su ON su.id = r.submitted_by
LEFT JOIN users au ON au.id = r.approved_by
LEFT JOIN users pu ON pu.id = r.posted_by
WHERE r.id = @id AND r.company_id = @company_id;

-- name: LockGoodsReceipt :one
SELECT * FROM goods_receipts WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: LockGoodsReceipts :many
-- 退出過帳時鎖定來源進貨單(依 id 排序)
SELECT id, doc_no, doc_type, status FROM goods_receipts
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: CreateGoodsReceipt :one
INSERT INTO goods_receipts (company_id, doc_type, doc_no, doc_date, supplier_id, warehouse_id, currency,
                            exchange_rate, tax_type_id, tax_rate, payment_term_id, invoice_no, untaxed_amount,
                            tax_amount, total_amount, base_untaxed, base_tax, base_total, note, created_by,
                            updated_by)
VALUES (@company_id, @doc_type, @doc_no, @doc_date, @supplier_id, @warehouse_id, @currency, @exchange_rate,
        @tax_type_id, @tax_rate, sqlc.narg(payment_term_id), @invoice_no, @untaxed_amount, @tax_amount,
        @total_amount, @base_untaxed, @base_tax, @base_total, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateGoodsReceiptHeader :one
UPDATE goods_receipts
SET doc_date = @doc_date, supplier_id = @supplier_id, warehouse_id = @warehouse_id, currency = @currency,
    exchange_rate = @exchange_rate, tax_type_id = @tax_type_id, tax_rate = @tax_rate,
    payment_term_id = sqlc.narg(payment_term_id), invoice_no = @invoice_no, untaxed_amount = @untaxed_amount,
    tax_amount = @tax_amount, total_amount = @total_amount, base_untaxed = @base_untaxed, base_tax = @base_tax,
    base_total = @base_total, note = @note, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetGoodsReceiptStatus :one
UPDATE goods_receipts
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

-- name: ListGoodsReceiptLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.item_type,
       u.name AS unit_name, bu.name AS base_unit_name,
       po.doc_no AS po_no, src.doc_no AS source_receipt_no
FROM goods_receipt_lines l
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
JOIN units bu ON bu.id = i.base_unit_id
LEFT JOIN purchase_order_lines pol ON pol.id = l.po_line_id
LEFT JOIN purchase_orders po ON po.id = pol.order_id
LEFT JOIN goods_receipt_lines srl ON srl.id = l.receipt_line_id
LEFT JOIN goods_receipts src ON src.id = srl.receipt_id
WHERE l.receipt_id = @receipt_id
ORDER BY l.line_no;

-- name: DeleteGoodsReceiptLines :exec
DELETE FROM goods_receipt_lines WHERE receipt_id = @receipt_id;

-- name: AddGoodsReceiptLine :exec
INSERT INTO goods_receipt_lines (receipt_id, line_no, item_id, unit_id, qty, factor, base_qty, unit_price, amount,
                                 base_amount, po_line_id, receipt_line_id, note)
VALUES (@receipt_id, @line_no, @item_id, @unit_id, @qty, @factor, @base_qty, @unit_price, @amount, @base_amount,
        sqlc.narg(po_line_id), sqlc.narg(receipt_line_id), @note);

-- name: PoLineRefs :many
-- 進貨明細引用的採購明細,以及「其他」已過帳進貨單的已交量
SELECT l.id, l.order_id, o.doc_no, o.status, o.supplier_id, o.currency, l.item_id, l.unit_id, l.qty,
       (SELECT COALESCE(SUM(rl.qty), 0) FROM goods_receipt_lines rl
        JOIN goods_receipts r ON r.id = rl.receipt_id
        WHERE rl.po_line_id = l.id AND r.status = 'posted' AND r.id <> @exclude_receipt_id::bigint)::numeric
           AS received_qty
FROM purchase_order_lines l
JOIN purchase_orders o ON o.id = l.order_id
WHERE o.company_id = @company_id AND l.id = ANY(@ids::bigint[]);

-- name: ReceiptLineRefs :many
-- 退出明細引用的進貨明細,以及「其他」已過帳退出單的已退量
SELECT l.id, l.receipt_id, r.doc_no, r.doc_type, r.status, r.supplier_id, r.currency, l.item_id, l.unit_id, l.qty,
       (SELECT COALESCE(SUM(xl.qty), 0) FROM goods_receipt_lines xl
        JOIN goods_receipts x ON x.id = xl.receipt_id
        WHERE xl.receipt_line_id = l.id AND x.status = 'posted' AND x.id <> @exclude_receipt_id::bigint)::numeric
           AS returned_qty
FROM goods_receipt_lines l
JOIN goods_receipts r ON r.id = l.receipt_id
WHERE r.company_id = @company_id AND l.id = ANY(@ids::bigint[]);

-- name: ReceiptReturnNo :one
-- 進貨單是否已被未作廢的退出單引用(反過帳前檢查)
SELECT x.doc_no FROM goods_receipt_lines xl
JOIN goods_receipts x ON x.id = xl.receipt_id
JOIN goods_receipt_lines l ON l.id = xl.receipt_line_id
WHERE l.receipt_id = @receipt_id AND x.status <> 'voided'
LIMIT 1;

-- name: ReturnableReceiptLines :many
-- 可退貨的進貨明細:已過帳進貨單中尚未退完的明細(退出單帶入用)
SELECT l.id AS receipt_line_id, r.id AS receipt_id, r.doc_no, r.doc_date, r.warehouse_id, l.line_no,
       l.item_id, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, l.unit_id, u.name AS unit_name,
       l.qty, l.unit_price, ret.returned_qty, (l.qty - ret.returned_qty)::numeric AS remaining_qty
FROM goods_receipt_lines l
JOIN goods_receipts r ON r.id = l.receipt_id
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(xl.qty), 0)::numeric AS returned_qty FROM goods_receipt_lines xl
    JOIN goods_receipts x ON x.id = xl.receipt_id
    WHERE xl.receipt_line_id = l.id AND x.status = 'posted'
) ret
WHERE r.company_id = @company_id AND r.doc_type = 'receipt' AND r.status = 'posted'
  AND r.supplier_id = @supplier_id AND r.currency = @currency AND l.qty > ret.returned_qty
  AND (sqlc.narg(keyword)::text IS NULL
       OR r.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY r.doc_date DESC, r.doc_no DESC, l.line_no
LIMIT 200;

-- name: SupplierForDoc :one
SELECT id, code, name, is_active FROM suppliers WHERE id = @id AND company_id = @company_id;

-- name: SupplierOptions :many
-- 開單選供應商:只需登入,只回傳啟用中的精簡欄位
SELECT id, code, name, short_name, currency, tax_type_id, payment_term_id
FROM suppliers
WHERE company_id = @company_id AND is_active
  AND (sqlc.narg(keyword)::text IS NULL
       OR code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR short_name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY code
LIMIT 20;

-- ======== 應付帳款 ========

-- name: InsertPayable :exec
INSERT INTO accounts_payable (company_id, supplier_id, source_type, source_id, source_no, doc_date, due_date,
                              currency, exchange_rate, amount, base_amount, created_by)
VALUES (@company_id, @supplier_id, @source_type, @source_id, @source_no, @doc_date, @due_date, @currency,
        @exchange_rate, @amount, @base_amount, sqlc.narg(created_by));

-- name: LockPayableBySource :one
SELECT * FROM accounts_payable WHERE source_type = @source_type AND source_id = @source_id FOR UPDATE;

-- name: DeletePayable :exec
DELETE FROM accounts_payable WHERE id = @id;

-- name: ListPayables :many
SELECT p.*, s.code AS supplier_code, s.name AS supplier_name
FROM accounts_payable p
JOIN suppliers s ON s.id = p.supplier_id
WHERE p.company_id = @company_id
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR p.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR p.source_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR p.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR p.doc_date <= sqlc.narg(to_date))
  AND (NOT @open_only::boolean OR p.amount <> p.paid_amount)
ORDER BY p.due_date, p.id
LIMIT @lim OFFSET @off;

-- name: CountPayables :one
-- base_amount_sum:未沖餘額的本位幣合計(依原幣餘額比例換算)
SELECT count(*) AS total,
       COALESCE(SUM(CASE WHEN p.amount = 0 THEN 0 ELSE p.base_amount * (p.amount - p.paid_amount) / p.amount END), 0)::numeric AS base_amount_sum
FROM accounts_payable p
WHERE p.company_id = @company_id
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR p.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR p.source_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR p.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR p.doc_date <= sqlc.narg(to_date))
  AND (NOT @open_only::boolean OR p.amount <> p.paid_amount);
