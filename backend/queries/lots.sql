-- ======== 批號與效期(D63) ========

-- name: FindOrCreateLot :one
INSERT INTO item_lots (company_id, item_id, lot_no, expiry_date)
VALUES (@company_id, @item_id, @lot_no, sqlc.narg(expiry_date))
ON CONFLICT (company_id, item_id, lot_no) DO UPDATE SET lot_no = item_lots.lot_no
RETURNING *;

-- name: GetLotByNo :one
SELECT * FROM item_lots WHERE company_id = @company_id AND item_id = @item_id AND lot_no = @lot_no;

-- name: GetLot :one
SELECT * FROM item_lots WHERE id = @id AND company_id = @company_id;

-- name: ListLotsByIDs :many
SELECT * FROM item_lots WHERE id = ANY(@ids::bigint[]);

-- name: EnsureLotBalance :exec
INSERT INTO inventory_lot_balances (company_id, item_id, warehouse_id, lot_id, qty)
VALUES (@company_id, @item_id, @warehouse_id, @lot_id, 0)
ON CONFLICT (lot_id, warehouse_id) DO NOTHING;

-- name: LockLotBalance :one
SELECT qty FROM inventory_lot_balances WHERE lot_id = @lot_id AND warehouse_id = @warehouse_id FOR UPDATE;

-- name: SetLotBalance :exec
UPDATE inventory_lot_balances SET qty = @qty, updated_at = now() WHERE lot_id = @lot_id AND warehouse_id = @warehouse_id;

-- name: ListLotBalancesForAllocation :many
-- 先到期先出:有庫存的批號依效期由近到遠(沒有效期的排最後),同效期依批號建立順序;鎖定這些列
SELECT b.lot_id, b.qty, l.lot_no, l.expiry_date
FROM inventory_lot_balances b JOIN item_lots l ON l.id = b.lot_id
WHERE b.item_id = @item_id AND b.warehouse_id = @warehouse_id AND b.qty > 0
ORDER BY l.expiry_date NULLS LAST, l.id
FOR UPDATE OF b;

-- name: ListOpenLotTransactionsBySource :many
-- 某張單據尚未被沖銷的批號分錄(單據上顯示實際出了哪些批號)
SELECT t.source_line_id, t.qty, l.lot_no, l.expiry_date
FROM inventory_transactions t JOIN item_lots l ON l.id = t.lot_id
WHERE t.source_type = @source_type AND t.source_id = @source_id AND t.reversal_of IS NULL
  AND NOT EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
ORDER BY t.id;

-- name: ListLotBalances :many
-- 批號庫存:依效期排序;expiry_mode 為 expired(已過期)、expiring(today 到 until 內到期)或空白(全部)
SELECT b.lot_id, l.lot_no, l.expiry_date, b.item_id, i.code AS item_code, i.name AS item_name, i.spec AS item_spec,
       bu.name AS unit_name, b.warehouse_id, w.code AS warehouse_code, w.name AS warehouse_name, b.qty
FROM inventory_lot_balances b
JOIN item_lots l ON l.id = b.lot_id
JOIN items i ON i.id = b.item_id
JOIN units bu ON bu.id = i.base_unit_id
JOIN warehouses w ON w.id = b.warehouse_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(item_id)::bigint IS NULL OR b.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL OR l.lot_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (@include_zero::boolean OR b.qty > 0)
  AND (@expiry_mode::text = ''
       OR (@expiry_mode::text = 'expired' AND l.expiry_date < @today::date)
       OR (@expiry_mode::text = 'expiring' AND l.expiry_date >= @today::date AND l.expiry_date <= @until::date))
ORDER BY l.expiry_date NULLS LAST, i.code, l.lot_no, w.code
LIMIT @lim OFFSET @off;

-- name: CountLotBalances :one
SELECT count(*) FROM inventory_lot_balances b
JOIN item_lots l ON l.id = b.lot_id
JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(item_id)::bigint IS NULL OR b.item_id = sqlc.narg(item_id))
  AND (sqlc.narg(warehouse_id)::bigint IS NULL OR b.warehouse_id = sqlc.narg(warehouse_id))
  AND (sqlc.narg(keyword)::text IS NULL OR l.lot_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (@include_zero::boolean OR b.qty > 0)
  AND (@expiry_mode::text = ''
       OR (@expiry_mode::text = 'expired' AND l.expiry_date < @today::date)
       OR (@expiry_mode::text = 'expiring' AND l.expiry_date >= @today::date AND l.expiry_date <= @until::date));

-- name: GetLotDetail :one
SELECT l.*, i.code AS item_code, i.name AS item_name, i.spec AS item_spec, i.lot_control
FROM item_lots l JOIN items i ON i.id = l.item_id
WHERE l.id = @id AND l.company_id = @company_id;

-- name: LotLedger :many
-- 批號追溯:這個批號所有的異動(含沖銷),附上進貨的供應商與出貨的客戶
SELECT t.id, t.doc_date, t.qty, t.source_type, t.source_id, t.source_no, t.warehouse_id, w.name AS warehouse_name,
       t.reversal_of, COALESCE(s.name, '')::text AS supplier_name, COALESCE(c.name, '')::text AS customer_name,
       d.sales_user_id AS sales_user_id,
       (SELECT u.department_id FROM users u WHERE u.id = d.sales_user_id) AS sales_department_id
FROM inventory_transactions t
JOIN warehouses w ON w.id = t.warehouse_id
LEFT JOIN goods_receipts gr ON t.source_type IN ('goods_receipt', 'purchase_return') AND gr.id = t.source_id
LEFT JOIN suppliers s ON s.id = gr.supplier_id
LEFT JOIN deliveries d ON t.source_type IN ('delivery', 'sales_return') AND d.id = t.source_id
LEFT JOIN customers c ON c.id = d.customer_id
WHERE t.lot_id = @lot_id AND t.company_id = @company_id
ORDER BY t.doc_date, t.id;

-- name: ListLotOptions :many
-- 開單挑選批號:某倉庫有庫存的批號(先到期先出的順序)
SELECT b.lot_id, l.lot_no, l.expiry_date, b.qty
FROM inventory_lot_balances b JOIN item_lots l ON l.id = b.lot_id
WHERE b.company_id = @company_id AND b.item_id = @item_id AND b.warehouse_id = @warehouse_id AND b.qty > 0
ORDER BY l.expiry_date NULLS LAST, l.id;

-- name: ListItemLots :many
-- 料品所有已建立的批號(不論有無庫存),供輸入批號時參考
SELECT l.id, l.lot_no, l.expiry_date FROM item_lots l
WHERE l.company_id = @company_id AND l.item_id = @item_id
ORDER BY l.expiry_date NULLS LAST, l.id DESC LIMIT 50;
