-- name: ListUnits :many
SELECT * FROM units WHERE company_id = @company_id ORDER BY code;

-- name: GetUnit :one
SELECT * FROM units WHERE id = @id AND company_id = @company_id;

-- name: CountUnitsByIDs :one
SELECT count(*) FROM units WHERE company_id = @company_id AND id = ANY(@ids::bigint[]);

-- name: CreateUnit :one
INSERT INTO units (company_id, code, name, created_by, updated_by)
VALUES (@company_id, @code, @name, @created_by, @created_by)
RETURNING *;

-- name: UpdateUnit :one
UPDATE units
SET code = @code, name = @name, is_active = @is_active, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListItemCategories :many
SELECT c.*, (SELECT count(*) FROM items i WHERE i.category_id = c.id) AS item_count
FROM item_categories c
WHERE c.company_id = @company_id
ORDER BY c.sort_order, c.code;

-- name: GetItemCategory :one
SELECT * FROM item_categories WHERE id = @id AND company_id = @company_id;

-- name: CreateItemCategory :one
INSERT INTO item_categories (company_id, parent_id, code, name, sort_order, created_by, updated_by)
VALUES (@company_id, sqlc.narg(parent_id), @code, @name, @sort_order, @created_by, @created_by)
RETURNING *;

-- name: UpdateItemCategory :one
UPDATE item_categories
SET parent_id = sqlc.narg(parent_id), code = @code, name = @name, sort_order = @sort_order,
    is_active = @is_active, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: IsItemCategoryDescendant :one
WITH RECURSIVE tree (cat_id) AS (
    SELECT item_categories.id FROM item_categories WHERE item_categories.id = @ancestor_id::bigint
    UNION ALL
    SELECT item_categories.id FROM item_categories, tree WHERE item_categories.parent_id = tree.cat_id
)
SELECT EXISTS (SELECT 1 FROM tree WHERE cat_id = @candidate_id::bigint);

-- name: ListWarehouses :many
SELECT * FROM warehouses WHERE company_id = @company_id ORDER BY code;

-- name: GetWarehouse :one
SELECT * FROM warehouses WHERE id = @id AND company_id = @company_id;

-- name: CreateWarehouse :one
INSERT INTO warehouses (company_id, code, name, address, allow_negative, created_by, updated_by)
VALUES (@company_id, @code, @name, @address, @allow_negative, @created_by, @created_by)
RETURNING *;

-- name: UpdateWarehouse :one
UPDATE warehouses
SET code = @code, name = @name, address = @address, allow_negative = @allow_negative,
    is_active = @is_active, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;
