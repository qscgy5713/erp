-- 採購與銷售單據共用

-- name: ListTradeItems :many
-- 開單驗證料品(商品與服務皆可買賣)
SELECT i.id, i.code, i.name, i.item_type, i.base_unit_id, i.is_active
FROM items i
WHERE i.company_id = @company_id AND i.id = ANY(@ids::bigint[]);

-- name: TaxTypeForDoc :one
SELECT id, rate, is_active FROM tax_types WHERE id = @id AND company_id = @company_id;
