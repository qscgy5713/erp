-- name: ListSalesPrices :many
SELECT sp.id, sp.customer_id, c.name AS customer_name, sp.item_id, i.code AS item_code, i.name AS item_name,
       sp.currency, sp.min_qty, sp.unit_price, sp.valid_from, sp.valid_to, sp.note
FROM sales_prices sp
JOIN items i ON i.id = sp.item_id
LEFT JOIN customers c ON c.id = sp.customer_id
WHERE sp.company_id = @company_id
  AND (sqlc.narg('item_id')::bigint IS NULL OR sp.item_id = sqlc.narg('item_id'))
  AND (sqlc.narg('customer_id')::bigint IS NULL OR sp.customer_id = sqlc.narg('customer_id'))
ORDER BY i.code, sp.customer_id NULLS FIRST, sp.min_qty
LIMIT 500;

-- name: CreateSalesPrice :one
INSERT INTO sales_prices (company_id, customer_id, item_id, currency, min_qty, unit_price, valid_from, valid_to, note, created_by)
VALUES (@company_id, @customer_id, @item_id, @currency, @min_qty, @unit_price, @valid_from, @valid_to, @note, @created_by)
RETURNING id;

-- name: DeleteSalesPrice :execrows
DELETE FROM sales_prices WHERE id = @id AND company_id = @company_id;

-- name: LookupSalesPrice :one
-- 客戶專屬價優先於一般價;同層取門檻最高(最貼近數量)者,再取最新建立者
SELECT unit_price, customer_id, min_qty
FROM sales_prices
WHERE company_id = @company_id AND item_id = @item_id AND currency = @currency
  AND (customer_id IS NULL OR customer_id = @customer_id)
  AND min_qty <= @qty
  AND (valid_from IS NULL OR valid_from <= @on_date)
  AND (valid_to IS NULL OR valid_to >= @on_date)
ORDER BY (customer_id IS NULL), min_qty DESC, id DESC
LIMIT 1;

-- name: ItemListPrice :one
SELECT list_price FROM items WHERE id = @id AND company_id = @company_id;
