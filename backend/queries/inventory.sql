-- name: EnsureBalance :exec
INSERT INTO inventory_balances (company_id, item_id, warehouse_id, qty)
VALUES (@company_id, @item_id, @warehouse_id, 0)
ON CONFLICT (item_id, warehouse_id) DO NOTHING;

-- name: LockBalance :one
SELECT qty FROM inventory_balances WHERE item_id = @item_id AND warehouse_id = @warehouse_id FOR UPDATE;

-- name: SetBalance :exec
UPDATE inventory_balances SET qty = @qty, updated_at = now()
WHERE item_id = @item_id AND warehouse_id = @warehouse_id;

-- name: InsertInventoryTransaction :one
INSERT INTO inventory_transactions (company_id, item_id, warehouse_id, doc_date, qty, unit_cost, source_type,
                                    source_id, source_line_id, source_no, reversal_of, created_by, lot_id)
VALUES (@company_id, @item_id, @warehouse_id, @doc_date, @qty, sqlc.narg(unit_cost), @source_type,
        @source_id, sqlc.narg(source_line_id), @source_no, sqlc.narg(reversal_of), sqlc.narg(created_by),
        sqlc.narg(lot_id))
RETURNING id;

-- name: ListOpenTransactionsBySource :many
-- 某張單據尚未被沖銷的分錄(反過帳用)
SELECT t.* FROM inventory_transactions t
WHERE t.source_type = @source_type AND t.source_id = @source_id AND t.reversal_of IS NULL
  AND NOT EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
ORDER BY t.id;

-- name: OpenCountDocNo :one
-- 指定倉庫是否有進行中的盤點單(盤點凍結)
SELECT sd.doc_no FROM stock_documents sd
WHERE sd.company_id = @company_id AND sd.doc_type = 'count'
  AND sd.status IN ('draft', 'pending', 'approved')
  AND sd.warehouse_id = ANY(@warehouse_ids::bigint[])
  AND sd.id <> @exclude_id::bigint
LIMIT 1;

-- name: ListStockItems :many
-- 過帳 / 開單時驗證料品:須屬同公司
SELECT i.id, i.code, i.name, i.item_type, i.base_unit_id, i.is_active, i.lot_control
FROM items i
WHERE i.company_id = @company_id AND i.id = ANY(@ids::bigint[]);

-- name: ListItemUnitFactors :many
-- 料品可用的單位與換算倍數(基本單位倍數為 1)
SELECT i.id AS item_id, i.base_unit_id AS unit_id, 1::numeric AS factor
FROM items i WHERE i.id = ANY(@item_ids::bigint[])
UNION ALL
SELECT iu.item_id, iu.unit_id, iu.factor FROM item_units iu WHERE iu.item_id = ANY(@item_ids::bigint[]);

-- name: ListWarehouseFlags :many
SELECT w.id, w.code, w.name, w.allow_negative, w.is_active
FROM warehouses w
WHERE w.company_id = @company_id AND w.id = ANY(@ids::bigint[]);

-- name: ItemHasTransactions :one
SELECT EXISTS (SELECT 1 FROM inventory_transactions WHERE item_id = @item_id);

-- name: ListBalances :many
WITH RECURSIVE cats (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = sqlc.narg(category_id)::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, cats WHERE item_categories.parent_id = cats.cat_id
)
SELECT b.item_id, b.warehouse_id, b.qty, b.updated_at,
       i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.safety_stock,
       u.name AS unit_name, w.code AS warehouse_code, w.name AS warehouse_name,
       (SELECT COALESCE(SUM(b2.qty), 0) FROM inventory_balances b2 WHERE b2.item_id = b.item_id)::numeric AS item_total
FROM inventory_balances b
JOIN items i ON i.id = b.item_id
JOIN units u ON u.id = i.base_unit_id
JOIN warehouses w ON w.id = b.warehouse_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR i.category_id IN (SELECT cat_id FROM cats))
  AND (NOT @nonzero::boolean OR b.qty <> 0)
  -- 低於安全庫存:以料品在所有倉庫的合計判斷
  AND (NOT @below_safety::boolean OR (i.safety_stock > 0 AND
       (SELECT COALESCE(SUM(b3.qty), 0) FROM inventory_balances b3 WHERE b3.item_id = b.item_id) < i.safety_stock))
ORDER BY i.code, w.code
LIMIT @lim OFFSET @off;

-- name: CountBalances :one
WITH RECURSIVE cats (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = sqlc.narg(category_id)::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, cats WHERE item_categories.parent_id = cats.cat_id
)
SELECT count(*)
FROM inventory_balances b
JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR i.category_id IN (SELECT cat_id FROM cats))
  AND (NOT @nonzero::boolean OR b.qty <> 0)
  AND (NOT @below_safety::boolean OR (i.safety_stock > 0 AND
       (SELECT COALESCE(SUM(b3.qty), 0) FROM inventory_balances b3 WHERE b3.item_id = b.item_id) < i.safety_stock));

-- name: StockMovementSummary :many
-- 收發存:期初、本期收、本期發、期末(依單據日期)。
-- 反向分錄依「原始分錄方向」歸類,沖銷後收發相互抵銷,不會同時多出一筆收與一筆發。
SELECT t.item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       COALESCE(SUM(t.qty) FILTER (WHERE t.doc_date < @from_date), 0)::numeric AS opening_qty,
       COALESCE(SUM(t.qty) FILTER (WHERE t.doc_date >= @from_date AND (
           (t.reversal_of IS NULL AND t.qty > 0) OR (t.reversal_of IS NOT NULL AND t.qty < 0))), 0)::numeric AS in_qty,
       COALESCE(-SUM(t.qty) FILTER (WHERE t.doc_date >= @from_date AND (
           (t.reversal_of IS NULL AND t.qty < 0) OR (t.reversal_of IS NOT NULL AND t.qty > 0))), 0)::numeric AS out_qty,
       COALESCE(SUM(t.qty), 0)::numeric AS closing_qty
FROM inventory_transactions t
JOIN items i ON i.id = t.item_id
JOIN units u ON u.id = i.base_unit_id
WHERE t.company_id = @company_id
  AND t.doc_date <= @to_date
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR t.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
GROUP BY t.item_id, i.code, i.name, u.name
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountStockMovementSummary :one
SELECT count(DISTINCT t.item_id)
FROM inventory_transactions t
JOIN items i ON i.id = t.item_id
WHERE t.company_id = @company_id
  AND t.doc_date <= @to_date
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR t.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: ItemLedgerOpening :one
SELECT COALESCE(SUM(qty), 0)::numeric FROM inventory_transactions
WHERE company_id = @company_id AND item_id = @item_id AND doc_date < @from_date
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR warehouse_id = sqlc.narg(warehouse_id));

-- name: ItemLedger :many
-- 料品異動明細與結存(結存 = 期初 + 依日期、序號累計)
SELECT t.id, t.doc_date, t.qty, t.source_type, t.source_id, t.source_no, t.reversal_of, t.created_at,
       w.code AS warehouse_code, w.name AS warehouse_name,
       (SUM(t.qty) OVER (ORDER BY t.doc_date, t.id))::numeric AS running_qty
FROM inventory_transactions t
JOIN warehouses w ON w.id = t.warehouse_id
WHERE t.company_id = @company_id AND t.item_id = @item_id
  AND t.doc_date >= @from_date AND t.doc_date <= @to_date
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR t.warehouse_id = sqlc.narg(warehouse_id))
ORDER BY t.doc_date, t.id
LIMIT 1000;
