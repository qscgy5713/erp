-- ======== 儲位(D65) ========

-- name: ListBins :many
SELECT b.*, (SELECT COALESCE(SUM(x.qty), 0) FROM inventory_bin_balances x WHERE x.bin_id = b.id)::numeric AS stock_qty
FROM bins b
WHERE b.company_id = @company_id AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
ORDER BY b.warehouse_id, b.code;

-- name: GetBin :one
SELECT * FROM bins WHERE id = @id AND company_id = @company_id;

-- name: GetBinByCode :one
SELECT * FROM bins WHERE company_id = @company_id AND warehouse_id = @warehouse_id AND code = @code;

-- name: ListBinsByIDs :many
SELECT * FROM bins WHERE id = ANY(@ids::bigint[]);

-- name: CreateBin :one
INSERT INTO bins (company_id, warehouse_id, code, name, created_by, updated_by)
VALUES (@company_id, @warehouse_id, @code, @name, @actor_id, @actor_id) RETURNING *;

-- name: UpdateBin :one
UPDATE bins SET name = @name, is_active = @is_active, version = version + 1, updated_by = @actor_id
WHERE id = @id AND company_id = @company_id AND version = @version RETURNING *;

-- name: BinHasStock :one
SELECT EXISTS (SELECT 1 FROM inventory_bin_balances WHERE bin_id = @bin_id AND qty <> 0);

-- name: BinHasTransactions :one
SELECT EXISTS (SELECT 1 FROM inventory_transactions WHERE bin_id = @bin_id);

-- name: DeleteBin :execrows
DELETE FROM bins WHERE id = @id AND company_id = @company_id;

-- name: WarehouseHasStock :one
SELECT EXISTS (SELECT 1 FROM inventory_balances WHERE warehouse_id = @warehouse_id AND qty <> 0);

-- name: WarehouseHasBinStock :one
SELECT EXISTS (SELECT 1 FROM inventory_bin_balances WHERE warehouse_id = @warehouse_id AND qty <> 0);

-- name: SetWarehouseUseBins :exec
UPDATE warehouses SET use_bins = @use_bins WHERE id = @id AND company_id = @company_id;

-- name: EnsureBinBalance :exec
INSERT INTO inventory_bin_balances (company_id, item_id, warehouse_id, bin_id, qty)
VALUES (@company_id, @item_id, @warehouse_id, @bin_id, 0) ON CONFLICT (bin_id, item_id) DO NOTHING;

-- name: LockBinBalance :one
SELECT qty FROM inventory_bin_balances WHERE bin_id = @bin_id AND item_id = @item_id FOR UPDATE;

-- name: SetBinBalance :exec
UPDATE inventory_bin_balances SET qty = @qty, updated_at = now() WHERE bin_id = @bin_id AND item_id = @item_id;

-- name: ListBinBalancesForAllocation :many
-- 出庫自動分配:庫存多的儲位先出(同量依儲位代號);鎖定這些列
SELECT b.bin_id, b.qty, bn.code
FROM inventory_bin_balances b JOIN bins bn ON bn.id = b.bin_id
WHERE b.item_id = @item_id AND b.warehouse_id = @warehouse_id AND b.qty > 0 AND bn.is_active
ORDER BY b.qty DESC, bn.code
FOR UPDATE OF b;

-- name: ListBinStock :many
-- 儲位庫存報表
SELECT b.bin_id, bn.code AS bin_code, bn.name AS bin_name, b.warehouse_id, w.code AS warehouse_code, w.name AS warehouse_name,
       b.item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name, b.qty
FROM inventory_bin_balances b
JOIN bins bn ON bn.id = b.bin_id JOIN warehouses w ON w.id = b.warehouse_id
JOIN items i ON i.id = b.item_id JOIN units u ON u.id = i.base_unit_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(bin_id)::bigint IS NULL OR b.bin_id = sqlc.narg(bin_id))
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR bn.code ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (@include_zero::boolean OR b.qty > 0)
ORDER BY w.code, bn.code, i.code
LIMIT @lim OFFSET @off;

-- name: CountBinStock :one
SELECT count(*) FROM inventory_bin_balances b
JOIN bins bn ON bn.id = b.bin_id JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(bin_id)::bigint IS NULL OR b.bin_id = sqlc.narg(bin_id))
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR bn.code ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (@include_zero::boolean OR b.qty > 0);

-- name: BinBalanceMismatches :one
-- 完整性:①儲位現有量 = 流水帳該儲位的合計;②啟用儲位的倉庫,各儲位合計 = 料品現有量。筆數應為 0
SELECT (
    (SELECT count(*) FROM (
        SELECT b.bin_id, b.item_id FROM inventory_bin_balances b
        LEFT JOIN (SELECT t.bin_id, t.item_id, SUM(t.qty) AS q FROM inventory_transactions t
                   WHERE t.company_id = @company_id AND t.bin_id IS NOT NULL GROUP BY t.bin_id, t.item_id) x
               ON x.bin_id = b.bin_id AND x.item_id = b.item_id
        WHERE b.company_id = @company_id AND b.qty <> COALESCE(x.q, 0)
    ) a)
    +
    (SELECT count(*) FROM (
        SELECT ib.item_id, ib.warehouse_id FROM inventory_balances ib JOIN warehouses w ON w.id = ib.warehouse_id
        LEFT JOIN (SELECT bb.item_id, bb.warehouse_id, SUM(bb.qty) AS q FROM inventory_bin_balances bb
                   WHERE bb.company_id = @company_id GROUP BY bb.item_id, bb.warehouse_id) y
               ON y.item_id = ib.item_id AND y.warehouse_id = ib.warehouse_id
        WHERE ib.company_id = @company_id AND w.use_bins AND ib.qty <> COALESCE(y.q, 0)
    ) c)
)::bigint AS mismatches;
