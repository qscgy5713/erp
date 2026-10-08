-- name: ListStockDocuments :many
SELECT d.id, d.doc_type, d.doc_no, d.doc_date, d.warehouse_id, d.to_warehouse_id, d.status, d.note,
       d.version, d.updated_at, w.name AS warehouse_name, tw.name AS to_warehouse_name,
       cu.name AS created_by_name,
       (SELECT count(*) FROM stock_document_lines l WHERE l.document_id = d.id) AS line_count
FROM stock_documents d
JOIN warehouses w ON w.id = d.warehouse_id
LEFT JOIN warehouses tw ON tw.id = d.to_warehouse_id
LEFT JOIN users cu ON cu.id = d.created_by
WHERE d.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR d.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(warehouse_id)::bigint IS NULL
       OR d.warehouse_id = sqlc.narg(warehouse_id) OR d.to_warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL OR d.doc_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR d.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR d.doc_date <= sqlc.narg(to_date))
ORDER BY d.doc_date DESC, d.id DESC
LIMIT @lim OFFSET @off;

-- name: CountStockDocuments :one
SELECT count(*) FROM stock_documents d
WHERE d.company_id = @company_id
  AND (sqlc.narg(doc_type)::text IS NULL OR d.doc_type = sqlc.narg(doc_type))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(warehouse_id)::bigint IS NULL
       OR d.warehouse_id = sqlc.narg(warehouse_id) OR d.to_warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL OR d.doc_no ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR d.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR d.doc_date <= sqlc.narg(to_date));

-- name: GetStockDocument :one
SELECT d.*, w.name AS warehouse_name, tw.name AS to_warehouse_name, c.name AS category_name,
       cu.name AS created_by_name, su.name AS submitted_by_name, au.name AS approved_by_name,
       pu.name AS posted_by_name
FROM stock_documents d
JOIN warehouses w ON w.id = d.warehouse_id
LEFT JOIN warehouses tw ON tw.id = d.to_warehouse_id
LEFT JOIN item_categories c ON c.id = d.category_id
LEFT JOIN users cu ON cu.id = d.created_by
LEFT JOIN users su ON su.id = d.submitted_by
LEFT JOIN users au ON au.id = d.approved_by
LEFT JOIN users pu ON pu.id = d.posted_by
WHERE d.id = @id AND d.company_id = @company_id;

-- name: LockStockDocument :one
SELECT * FROM stock_documents WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: CreateStockDocument :one
INSERT INTO stock_documents (company_id, doc_type, doc_no, doc_date, warehouse_id, to_warehouse_id, category_id,
                             note, created_by, updated_by)
VALUES (@company_id, @doc_type, @doc_no, @doc_date, @warehouse_id, sqlc.narg(to_warehouse_id),
        sqlc.narg(category_id), @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateStockDocumentHeader :one
-- 只有草稿可修改;倉庫在建立後不可變更(盤點快照、調撥方向皆依此)
UPDATE stock_documents
SET doc_date = @doc_date, to_warehouse_id = sqlc.narg(to_warehouse_id), note = @note,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetStockDocumentStatus :one
UPDATE stock_documents
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

-- name: ListStockDocumentLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec,
       u.name AS unit_name, bu.name AS base_unit_name
FROM stock_document_lines l
JOIN items i ON i.id = l.item_id
JOIN units u ON u.id = l.unit_id
JOIN units bu ON bu.id = i.base_unit_id
WHERE l.document_id = @document_id
ORDER BY l.line_no;

-- name: DeleteStockDocumentLines :exec
DELETE FROM stock_document_lines WHERE document_id = @document_id;

-- name: AddStockDocumentLine :exec
INSERT INTO stock_document_lines (document_id, line_no, item_id, unit_id, qty, factor, base_qty, system_qty, note)
VALUES (@document_id, @line_no, @item_id, @unit_id, sqlc.narg(qty), @factor, sqlc.narg(base_qty),
        sqlc.narg(system_qty), @note);

-- name: CountSnapshot :many
-- 盤點建立時的帳面數量快照:該倉庫有現有量紀錄的商品(可限分類,含下層)
WITH RECURSIVE cats (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = sqlc.narg(category_id)::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, cats WHERE item_categories.parent_id = cats.cat_id
)
SELECT b.item_id, i.base_unit_id, b.qty
FROM inventory_balances b
JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id AND b.warehouse_id = @warehouse_id AND i.item_type = 'goods'
  AND (sqlc.narg(category_id)::bigint IS NULL OR i.category_id IN (SELECT cat_id FROM cats))
ORDER BY i.code;

-- name: GetBalanceQty :one
SELECT COALESCE((SELECT qty FROM inventory_balances WHERE item_id = @item_id AND warehouse_id = @warehouse_id), 0)::numeric;
