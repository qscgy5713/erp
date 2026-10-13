-- ======== 在製品(D101) ========

-- name: WipWarehouse :one
SELECT * FROM warehouses WHERE company_id = @company_id AND kind = 'wip';

-- name: CreateWipWarehouse :one
INSERT INTO warehouses (company_id, code, name, kind, created_by, updated_by)
VALUES (@company_id, 'WIP', '在製品倉', 'wip', @actor_id, @actor_id)
RETURNING *;

-- name: CreateWipTxn :one
INSERT INTO work_order_wip_txns (company_id, work_order_id, kind, doc_no, doc_date, note, created_by)
VALUES (@company_id, @work_order_id, @kind, @doc_no, @doc_date, @note, @actor_id)
RETURNING *;

-- name: AddWipTxnLine :exec
INSERT INTO work_order_wip_txn_lines (txn_id, line_no, item_id, qty, lot_no, bin_code)
VALUES (@txn_id, @line_no, @item_id, @qty, @lot_no, @bin_code);

-- name: LockWipTxn :one
SELECT * FROM work_order_wip_txns WHERE id = @id AND work_order_id = @work_order_id AND company_id = @company_id FOR UPDATE;

-- name: VoidWipTxn :exec
UPDATE work_order_wip_txns SET status = 'voided', voided_by = @actor_id, voided_at = now() WHERE id = @id;

-- name: ListWipTxns :many
SELECT t.*, cu.name AS created_by_name, vu.name AS voided_by_name
FROM work_order_wip_txns t LEFT JOIN users cu ON cu.id = t.created_by LEFT JOIN users vu ON vu.id = t.voided_by
WHERE t.work_order_id = @work_order_id ORDER BY t.id;

-- name: ListWipTxnLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM work_order_wip_txn_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.txn_id = @txn_id ORDER BY l.line_no;

-- name: CountWipTxnsOfOrder :one
-- 工單有效的領料 / 退料筆數:有的話不能用一次完工 / 反過帳 / 取消核准 / 作廢
SELECT count(*) FROM work_order_wip_txns WHERE work_order_id = @work_order_id AND status = 'posted';

-- name: WipHeldQty :many
-- 工單目前還在在製品倉的材料量:領料 − 退料 − 完工回報從在製品扣掉的
SELECT x.item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name, SUM(x.qty)::numeric AS qty FROM (
    SELECT l.item_id, l.qty * CASE WHEN t.kind = 'issue' THEN 1 ELSE -1 END AS qty
    FROM work_order_wip_txn_lines l JOIN work_order_wip_txns t ON t.id = l.txn_id
    WHERE t.work_order_id = @work_order_id AND t.status = 'posted'
  UNION ALL
    SELECT l.item_id, -l.from_wip
    FROM work_order_completion_lines l JOIN work_order_completions c ON c.id = l.completion_id
    WHERE c.work_order_id = @work_order_id AND c.status = 'posted' AND l.from_wip <> 0
) x JOIN items i ON i.id = x.item_id JOIN units u ON u.id = i.base_unit_id
GROUP BY x.item_id, i.code, i.name, u.name ORDER BY i.code;

-- name: ListWipReport :many
-- 在製品清單:進行中(已核准)的工單目前還在在製品倉的材料;金額 = 數量 × 最近一次月結的平均成本(沒有就 0)
SELECT w.id AS work_order_id, w.doc_no, w.doc_date, wi.code AS item_code, wi.name AS item_name,
       mi.id AS material_id, mi.code AS material_code, mi.name AS material_name, u.name AS unit_name,
       h.qty,
       COALESCE((SELECT ic.avg_cost FROM item_costs ic JOIN cost_closings cc ON cc.id = ic.closing_id
                 WHERE ic.item_id = mi.id AND cc.company_id = w.company_id ORDER BY cc.period DESC LIMIT 1), 0)::numeric AS avg_cost
FROM work_orders w
JOIN items wi ON wi.id = w.item_id
JOIN LATERAL (
    SELECT x.item_id, SUM(x.qty)::numeric AS qty FROM (
        SELECT l.item_id, l.qty * CASE WHEN t.kind = 'issue' THEN 1 ELSE -1 END AS qty
        FROM work_order_wip_txn_lines l JOIN work_order_wip_txns t ON t.id = l.txn_id
        WHERE t.work_order_id = w.id AND t.status = 'posted'
      UNION ALL
        SELECT l.item_id, -l.from_wip
        FROM work_order_completion_lines l JOIN work_order_completions c ON c.id = l.completion_id
        WHERE c.work_order_id = w.id AND c.status = 'posted' AND l.from_wip <> 0
    ) x GROUP BY x.item_id HAVING SUM(x.qty) <> 0
) h ON TRUE
JOIN items mi ON mi.id = h.item_id JOIN units u ON u.id = mi.base_unit_id
WHERE w.company_id = @company_id AND w.status = 'approved'
ORDER BY w.doc_date, w.id, mi.code;
