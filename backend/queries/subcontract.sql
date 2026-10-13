-- ======== 委外加工(D100) ========

-- name: SubcontractWarehouseOf :one
SELECT * FROM warehouses WHERE company_id = @company_id AND supplier_id = @supplier_id AND kind = 'subcontract';

-- name: CreateSubcontractWarehouse :one
INSERT INTO warehouses (company_id, code, name, kind, supplier_id, created_by, updated_by)
VALUES (@company_id, @code, @name, 'subcontract', @supplier_id, @actor_id, @actor_id)
RETURNING *;

-- name: ListSubcontractOrders :many
SELECT o.id, o.doc_no, o.doc_date, o.status, o.plan_qty, o.unit_fee, o.due_date, o.note, o.updated_at,
       COALESCE((SELECT SUM(t.qty) FROM subcontract_txns t WHERE t.order_id = o.id AND t.kind = 'receive' AND t.status = 'posted'), 0)::numeric AS received_qty,
       s.code AS supplier_code, s.name AS supplier_name, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM subcontract_orders o
JOIN suppliers s ON s.id = o.supplier_id JOIN items i ON i.id = o.item_id JOIN units u ON u.id = i.base_unit_id
WHERE o.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR s.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY o.doc_date DESC, o.id DESC
LIMIT @lim OFFSET @off;

-- name: CountSubcontractOrders :one
SELECT count(*) FROM subcontract_orders o
JOIN suppliers s ON s.id = o.supplier_id JOIN items i ON i.id = o.item_id
WHERE o.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
  AND (sqlc.narg(supplier_id)::bigint IS NULL OR o.supplier_id = sqlc.narg(supplier_id))
  AND (sqlc.narg(keyword)::text IS NULL OR o.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR s.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: GetSubcontractOrder :one
SELECT o.*, s.code AS supplier_code, s.name AS supplier_name, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       i.lot_control AS item_lot_control, iw.name AS issue_warehouse_name, rw.name AS receive_warehouse_name,
       sw.name AS sc_warehouse_name, cu.name AS created_by_name
FROM subcontract_orders o
JOIN suppliers s ON s.id = o.supplier_id JOIN items i ON i.id = o.item_id JOIN units u ON u.id = i.base_unit_id
JOIN warehouses iw ON iw.id = o.issue_warehouse_id JOIN warehouses rw ON rw.id = o.receive_warehouse_id
JOIN warehouses sw ON sw.id = o.sc_warehouse_id
LEFT JOIN users cu ON cu.id = o.created_by
WHERE o.id = @id AND o.company_id = @company_id;

-- name: LockSubcontractOrder :one
SELECT * FROM subcontract_orders WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: CreateSubcontractOrder :one
INSERT INTO subcontract_orders (company_id, doc_no, doc_date, supplier_id, item_id, plan_qty, unit_fee, issue_warehouse_id,
                                receive_warehouse_id, sc_warehouse_id, due_date, note, created_by, updated_by)
VALUES (@company_id, @doc_no, @doc_date, @supplier_id, @item_id, @plan_qty, @unit_fee, @issue_warehouse_id,
        @receive_warehouse_id, @sc_warehouse_id, sqlc.narg(due_date), @note, @actor_id, @actor_id)
RETURNING *;

-- name: UpdateSubcontractOrderHeader :one
UPDATE subcontract_orders SET doc_date = @doc_date, plan_qty = @plan_qty, unit_fee = @unit_fee,
    issue_warehouse_id = @issue_warehouse_id, receive_warehouse_id = @receive_warehouse_id,
    due_date = sqlc.narg(due_date), note = @note, version = version + 1, updated_by = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'open'
RETURNING *;

-- name: SetSubcontractOrderStatus :exec
UPDATE subcontract_orders SET status = @status, version = version + 1, updated_by = @actor_id::bigint WHERE id = @id;

-- name: DeleteSubcontractOrderLines :exec
DELETE FROM subcontract_order_lines WHERE order_id = @order_id;

-- name: AddSubcontractOrderLine :exec
INSERT INTO subcontract_order_lines (order_id, line_no, item_id, qty, note) VALUES (@order_id, @line_no, @item_id, @qty, @note);

-- name: ListSubcontractOrderLines :many
-- 預計用量,附累計發料(發料 − 退料)、累計耗用(收回的實際耗料)與目前在委外倉的庫存
SELECT l.line_no, l.item_id, l.qty, l.note, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       i.lot_control AS item_lot_control,
       COALESCE((SELECT SUM(x.qty * CASE t.kind WHEN 'issue' THEN 1 WHEN 'return' THEN -1 ELSE 0 END)
                 FROM subcontract_txn_lines x JOIN subcontract_txns t ON t.id = x.txn_id
                 WHERE t.order_id = l.order_id AND t.status = 'posted' AND x.item_id = l.item_id), 0)::numeric AS issued_qty,
       COALESCE((SELECT SUM(x.qty) FROM subcontract_txn_lines x JOIN subcontract_txns t ON t.id = x.txn_id
                 WHERE t.order_id = l.order_id AND t.status = 'posted' AND t.kind = 'receive' AND x.item_id = l.item_id), 0)::numeric AS consumed_qty
FROM subcontract_order_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.order_id = @order_id ORDER BY l.line_no;

-- name: SubcontractHeldQty :many
-- 委外單目前還在加工廠的材料數量(發料 − 退料 − 收回耗用),依料品
SELECT x.item_id, SUM(x.qty * CASE WHEN t.kind = 'issue' THEN 1 ELSE -1 END)::numeric AS qty
FROM subcontract_txn_lines x JOIN subcontract_txns t ON t.id = x.txn_id
WHERE t.order_id = @order_id AND t.status = 'posted'
GROUP BY x.item_id;

-- name: SubcontractReceivedQty :one
SELECT COALESCE(SUM(qty), 0)::numeric FROM subcontract_txns WHERE order_id = @order_id AND kind = 'receive' AND status = 'posted';

-- name: CountSubcontractTxns :one
SELECT count(*) FROM subcontract_txns WHERE order_id = @order_id AND status = 'posted';

-- name: CreateSubcontractTxn :one
INSERT INTO subcontract_txns (company_id, order_id, kind, doc_no, doc_date, qty, processing_fee, output_lot_no, output_expiry,
                              output_bin_code, note, created_by)
VALUES (@company_id, @order_id, @kind, @doc_no, @doc_date, @qty, @processing_fee, @output_lot_no, sqlc.narg(output_expiry),
        @output_bin_code, @note, @actor_id)
RETURNING *;

-- name: AddSubcontractTxnLine :exec
INSERT INTO subcontract_txn_lines (txn_id, line_no, item_id, qty, lot_no, bin_code) VALUES (@txn_id, @line_no, @item_id, @qty, @lot_no, @bin_code);

-- name: LockSubcontractTxn :one
SELECT * FROM subcontract_txns WHERE id = @id AND order_id = @order_id AND company_id = @company_id FOR UPDATE;

-- name: VoidSubcontractTxn :exec
UPDATE subcontract_txns SET status = 'voided', voided_by = @actor_id, voided_at = now() WHERE id = @id;

-- name: ListSubcontractTxns :many
SELECT t.*, cu.name AS created_by_name, vu.name AS voided_by_name
FROM subcontract_txns t LEFT JOIN users cu ON cu.id = t.created_by LEFT JOIN users vu ON vu.id = t.voided_by
WHERE t.order_id = @order_id ORDER BY t.id;

-- name: ListSubcontractTxnLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM subcontract_txn_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.txn_id = @txn_id ORDER BY l.line_no;

-- name: ListSubcontractTxnLots :many
-- 某次收回實際入庫與領用的批號(發料 / 退料是調撥,不另列)
SELECT t.source_type, t.item_id, l.lot_no, l.expiry_date, t.qty
FROM inventory_transactions t JOIN item_lots l ON l.id = t.lot_id
WHERE t.source_type IN ('sc_receive_issue', 'sc_receive_receipt') AND t.source_id = @txn_id
  AND t.reversal_of IS NULL AND NOT EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
ORDER BY t.id;
