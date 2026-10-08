-- name: ListItems :many
-- category_id 篩選包含所有下層分類
WITH RECURSIVE cats (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = sqlc.narg(category_id)::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, cats WHERE item_categories.parent_id = cats.cat_id
)
SELECT i.*, c.name AS category_name, u.name AS base_unit_name
FROM items i
LEFT JOIN item_categories c ON c.id = i.category_id
JOIN units u ON u.id = i.base_unit_id
WHERE i.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.spec ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.barcode = sqlc.narg(keyword))
  AND (sqlc.narg(category_id)::bigint IS NULL OR i.category_id IN (SELECT cat_id FROM cats))
  AND (sqlc.narg(item_type)::text IS NULL OR i.item_type = sqlc.narg(item_type))
  AND (sqlc.narg(is_active)::boolean IS NULL OR i.is_active = sqlc.narg(is_active))
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountItems :one
WITH RECURSIVE cats (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = sqlc.narg(category_id)::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, cats WHERE item_categories.parent_id = cats.cat_id
)
SELECT count(*)
FROM items i
WHERE i.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.spec ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.barcode = sqlc.narg(keyword))
  AND (sqlc.narg(category_id)::bigint IS NULL OR i.category_id IN (SELECT cat_id FROM cats))
  AND (sqlc.narg(item_type)::text IS NULL OR i.item_type = sqlc.narg(item_type))
  AND (sqlc.narg(is_active)::boolean IS NULL OR i.is_active = sqlc.narg(is_active));

-- name: GetItem :one
SELECT * FROM items WHERE id = @id AND company_id = @company_id;

-- name: CheckItemRefs :one
-- 參照的資料須存在且屬於同公司;未指定(NULL)視為通過
SELECT
    COALESCE(sqlc.narg(category_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM item_categories ic
        WHERE ic.id = sqlc.narg(category_id)::bigint AND ic.company_id = @company_id::bigint), FALSE)::boolean AS category_ok,
    EXISTS (
        SELECT 1 FROM units un
        WHERE un.id = @base_unit_id::bigint AND un.company_id = @company_id::bigint) AS base_unit_ok,
    COALESCE(sqlc.narg(tax_type_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM tax_types tt
        WHERE tt.id = sqlc.narg(tax_type_id)::bigint AND tt.company_id = @company_id::bigint), FALSE)::boolean AS tax_type_ok,
    COALESCE(sqlc.narg(warehouse_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM warehouses wh
        WHERE wh.id = sqlc.narg(warehouse_id)::bigint AND wh.company_id = @company_id::bigint), FALSE)::boolean AS warehouse_ok;

-- name: CreateItem :one
INSERT INTO items (company_id, code, name, spec, category_id, item_type, base_unit_id, barcode,
                   tax_type_id, default_warehouse_id, safety_stock, list_price, note, lot_control, created_by, updated_by)
VALUES (@company_id, @code, @name, @spec, sqlc.narg(category_id), @item_type, @base_unit_id, sqlc.narg(barcode),
        sqlc.narg(tax_type_id), sqlc.narg(default_warehouse_id), @safety_stock, @list_price, @note,
        COALESCE(NULLIF(@lot_control::text, ''), 'none'), @created_by, @created_by)
RETURNING *;

-- name: UpdateItem :one
-- 基本單位不在此修改:已有異動後改基本單位會讓歷史數量失真(M2 起由庫存模組把關)
UPDATE items
SET code = @code, name = @name, spec = @spec, category_id = sqlc.narg(category_id), item_type = @item_type,
    base_unit_id = @base_unit_id, barcode = sqlc.narg(barcode), tax_type_id = sqlc.narg(tax_type_id),
    default_warehouse_id = sqlc.narg(default_warehouse_id), safety_stock = @safety_stock,
    list_price = @list_price, note = @note, is_active = @is_active, lot_control = @lot_control,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListItemUnits :many
SELECT iu.*, u.code AS unit_code, u.name AS unit_name
FROM item_units iu
JOIN units u ON u.id = iu.unit_id
WHERE iu.item_id = ANY(@item_ids::bigint[])
ORDER BY iu.item_id, iu.factor;

-- name: DeleteItemUnits :exec
DELETE FROM item_units WHERE item_id = @item_id;

-- name: AddItemUnit :exec
INSERT INTO item_units (item_id, unit_id, factor, barcode)
VALUES (@item_id, @unit_id, @factor, sqlc.narg(barcode));
