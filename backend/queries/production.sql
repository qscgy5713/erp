-- ======== BOM ========

-- name: ListBoms :many
SELECT b.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       (SELECT count(*) FROM bom_lines l WHERE l.bom_id = b.id)::bigint AS line_count
FROM boms b JOIN items i ON i.id = b.item_id JOIN units u ON u.id = i.base_unit_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountBoms :one
SELECT count(*) FROM boms b JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: GetBom :one
SELECT b.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM boms b JOIN items i ON i.id = b.item_id JOIN units u ON u.id = i.base_unit_id
WHERE b.id = @id AND b.company_id = @company_id;

-- name: GetBomByItem :one
SELECT * FROM boms WHERE company_id = @company_id AND item_id = @item_id;

-- name: ListBomLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name, i.item_type, i.is_active AS item_active
FROM bom_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.bom_id = @bom_id ORDER BY l.line_no;

-- name: CreateBom :one
INSERT INTO boms (company_id, item_id, yield_qty, is_active, note, created_by, updated_by)
VALUES (@company_id, @item_id, @yield_qty, @is_active, @note, @actor_id, @actor_id) RETURNING *;

-- name: UpdateBom :one
UPDATE boms SET yield_qty = @yield_qty, is_active = @is_active, note = @note, version = version + 1, updated_by = @actor_id
WHERE id = @id AND company_id = @company_id AND version = @version RETURNING *;

-- name: DeleteBom :execrows
DELETE FROM boms WHERE id = @id AND company_id = @company_id;

-- name: DeleteBomLines :exec
DELETE FROM bom_lines WHERE bom_id = @bom_id;

-- name: AddBomLine :exec
INSERT INTO bom_lines (bom_id, line_no, item_id, qty, note) VALUES (@bom_id, @line_no, @item_id, @qty, @note);

-- name: AllBomEdges :many
-- 全公司所有 BOM 的 (成品 → 材料) 關係,檢查循環用
SELECT b.item_id AS parent_id, l.item_id AS child_id
FROM boms b JOIN bom_lines l ON l.bom_id = b.id WHERE b.company_id = @company_id;

-- name: ListProductionItems :many
-- 檢查 BOM 與工單用:料品須為同公司、商品類、啟用
SELECT id, code, name, item_type, is_active, lot_control FROM items
WHERE company_id = @company_id AND id = ANY(@ids::bigint[]);

-- name: BomUsedBy :many
-- 這個料品被哪些成品的 BOM 當材料
SELECT i.code, i.name FROM bom_lines l JOIN boms b ON b.id = l.bom_id JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id AND l.item_id = @item_id ORDER BY i.code LIMIT 20;

-- ======== 工單 ========

-- name: ListWorkOrders :many
SELECT w.id, w.doc_no, w.doc_date, w.status, w.plan_qty, w.due_date, w.processing_cost, w.note, w.updated_at,
       i.code AS item_code, i.name AS item_name, u.name AS unit_name, wh.name AS warehouse_name,
       cu.name AS created_by_name
FROM work_orders w
JOIN items i ON i.id = w.item_id JOIN units u ON u.id = i.base_unit_id
JOIN warehouses wh ON wh.id = w.warehouse_id
LEFT JOIN users cu ON cu.id = w.created_by
WHERE w.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR w.status = sqlc.narg(status))
  AND (sqlc.narg(item_id)::bigint IS NULL OR w.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(from_date)::date IS NULL OR w.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR w.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(keyword)::text IS NULL OR w.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY w.doc_date DESC, w.id DESC
LIMIT @lim OFFSET @off;

-- name: CountWorkOrders :one
SELECT count(*) FROM work_orders w JOIN items i ON i.id = w.item_id
WHERE w.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR w.status = sqlc.narg(status))
  AND (sqlc.narg(item_id)::bigint IS NULL OR w.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(from_date)::date IS NULL OR w.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR w.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(keyword)::text IS NULL OR w.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: GetWorkOrder :one
SELECT w.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name, i.lot_control AS item_lot_control,
       wh.name AS warehouse_name, mwh.name AS material_warehouse_name,
       cu.name AS created_by_name, su.name AS submitted_by_name, au.name AS approved_by_name, pu.name AS posted_by_name
FROM work_orders w
JOIN items i ON i.id = w.item_id JOIN units u ON u.id = i.base_unit_id
JOIN warehouses wh ON wh.id = w.warehouse_id JOIN warehouses mwh ON mwh.id = w.material_warehouse_id
LEFT JOIN users cu ON cu.id = w.created_by LEFT JOIN users su ON su.id = w.submitted_by
LEFT JOIN users au ON au.id = w.approved_by LEFT JOIN users pu ON pu.id = w.posted_by
WHERE w.id = @id AND w.company_id = @company_id;

-- name: LockWorkOrder :one
SELECT * FROM work_orders WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: ListWorkOrderLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name, i.lot_control AS item_lot_control,
       COALESCE((SELECT b.qty FROM inventory_balances b WHERE b.item_id = l.item_id AND b.warehouse_id = @material_warehouse_id), 0)::numeric AS on_hand
FROM work_order_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.work_order_id = @work_order_id ORDER BY l.line_no;

-- name: CreateWorkOrder :one
INSERT INTO work_orders (company_id, doc_no, doc_date, item_id, plan_qty, warehouse_id, material_warehouse_id,
                         processing_cost, output_lot_no, output_expiry, due_date, note, created_by, updated_by)
VALUES (@company_id, @doc_no, @doc_date, @item_id, @plan_qty, @warehouse_id, @material_warehouse_id,
        @processing_cost, @output_lot_no, sqlc.narg(output_expiry), sqlc.narg(due_date), @note, @actor_id, @actor_id)
RETURNING *;

-- name: UpdateWorkOrderHeader :one
UPDATE work_orders SET doc_date = @doc_date, item_id = @item_id, plan_qty = @plan_qty, warehouse_id = @warehouse_id,
    material_warehouse_id = @material_warehouse_id, processing_cost = @processing_cost, output_lot_no = @output_lot_no,
    output_expiry = sqlc.narg(output_expiry), due_date = sqlc.narg(due_date), note = @note,
    version = version + 1, updated_by = @actor_id
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: DeleteWorkOrderLines :exec
DELETE FROM work_order_lines WHERE work_order_id = @work_order_id;

-- name: AddWorkOrderLine :exec
INSERT INTO work_order_lines (work_order_id, line_no, item_id, qty, lot_no, note)
VALUES (@work_order_id, @line_no, @item_id, @qty, @lot_no, @note);

-- name: SetWorkOrderStatus :one
UPDATE work_orders
SET status       = @status,
    submitted_by = CASE WHEN @action::text = 'submit' THEN @actor_id::bigint ELSE submitted_by END,
    submitted_at = CASE WHEN @action::text = 'submit' THEN now() ELSE submitted_at END,
    approved_by  = CASE WHEN @action::text = 'approve' THEN @actor_id::bigint
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_by END,
    approved_at  = CASE WHEN @action::text = 'approve' THEN now()
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_at END,
    posted_by    = CASE WHEN @action::text = 'post' THEN @actor_id::bigint WHEN @action::text = 'unpost' THEN NULL ELSE posted_by END,
    posted_at    = CASE WHEN @action::text = 'post' THEN now() WHEN @action::text = 'unpost' THEN NULL ELSE posted_at END,
    version      = version + 1,
    updated_by   = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListWorkOrderLots :many
-- 已過帳工單的成品批號與材料實際領用的批號
SELECT t.source_type, t.item_id, l.lot_no, l.expiry_date, t.qty
FROM inventory_transactions t JOIN item_lots l ON l.id = t.lot_id
WHERE t.source_type IN ('work_order_issue', 'work_order_receipt') AND t.source_id = @work_order_id
  AND t.reversal_of IS NULL AND NOT EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
ORDER BY t.id;

-- ======== 月結成本:生產 ========

-- name: ProductionConsumption :many
-- 期間內已過帳工單領用的材料:(成品, 材料) 的領用數量(負),含沖銷分錄所以反過帳的工單會互抵為 0
SELECT w.item_id AS output_item_id, t.item_id AS material_id, SUM(t.qty)::numeric AS qty
FROM inventory_transactions t JOIN work_orders w ON w.id = t.source_id
WHERE t.company_id = @company_id AND t.source_type = 'work_order_issue'
  AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
GROUP BY w.item_id, t.item_id
HAVING SUM(t.qty) <> 0;

-- name: ProductionProcessing :many
-- 期間內已完工(過帳中)的工單加工費,依成品彙總
SELECT w.item_id, SUM(w.processing_cost)::numeric AS processing
FROM work_orders w
WHERE w.company_id = @company_id AND w.status = 'posted'
  AND w.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
GROUP BY w.item_id;

-- name: WritebackProduceCost :exec
-- 完工入庫的單位成本 = 該成品當月的完工成本 ÷ 完工數量
UPDATE inventory_transactions SET unit_cost = @unit_cost
WHERE company_id = @company_id AND item_id = @item_id AND source_type = 'work_order_receipt'
  AND doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date;

-- name: ItemHasWorkOrders :one
SELECT EXISTS (SELECT 1 FROM work_orders WHERE company_id = @company_id AND item_id = @item_id);
