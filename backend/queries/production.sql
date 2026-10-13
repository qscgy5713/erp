-- ======== BOM ========

-- name: ListBoms :many
SELECT b.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       (SELECT count(*) FROM bom_lines l WHERE l.bom_id = b.id)::bigint AS line_count
FROM boms b JOIN items i ON i.id = b.item_id JOIN units u ON u.id = i.base_unit_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY i.code, b.effective_from DESC
LIMIT @lim OFFSET @off;

-- name: CountBoms :one
SELECT count(*) FROM boms b JOIN items i ON i.id = b.item_id
WHERE b.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: GetBom :one
SELECT b.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM boms b JOIN items i ON i.id = b.item_id JOIN units u ON u.id = i.base_unit_id
WHERE b.id = @id AND b.company_id = @company_id;

-- name: GetBomForDate :one
-- 工單日期適用的 BOM:已生效且啟用中,生效日最晚的一份
SELECT * FROM boms
WHERE company_id = @company_id AND item_id = @item_id AND is_active AND effective_from <= @on_date
ORDER BY effective_from DESC LIMIT 1;

-- name: CountBomsOfItem :one
SELECT count(*) FROM boms WHERE company_id = @company_id AND item_id = @item_id;

-- name: ListBomLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name, i.item_type, i.is_active AS item_active
FROM bom_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.bom_id = @bom_id ORDER BY l.line_no;

-- name: CreateBom :one
INSERT INTO boms (company_id, item_id, yield_qty, effective_from, is_active, note, created_by, updated_by)
VALUES (@company_id, @item_id, @yield_qty, @effective_from, @is_active, @note, @actor_id, @actor_id) RETURNING *;

-- name: UpdateBom :one
UPDATE boms SET yield_qty = @yield_qty, effective_from = @effective_from, is_active = @is_active, note = @note, version = version + 1, updated_by = @actor_id
WHERE id = @id AND company_id = @company_id AND version = @version RETURNING *;

-- name: DeleteBom :execrows
DELETE FROM boms WHERE id = @id AND company_id = @company_id;

-- name: DeleteBomLines :exec
DELETE FROM bom_lines WHERE bom_id = @bom_id;

-- name: AddBomLine :exec
INSERT INTO bom_lines (bom_id, line_no, item_id, qty, scrap_pct, note) VALUES (@bom_id, @line_no, @item_id, @qty, @scrap_pct, @note);

-- name: AllBomEdges :many
-- 全公司所有 BOM 的 (成品 → 材料) 關係(含替代料),檢查循環用
SELECT b.id AS bom_id, b.item_id AS parent_id, l.item_id AS child_id
FROM boms b JOIN bom_lines l ON l.bom_id = b.id WHERE b.company_id = @company_id
UNION ALL
SELECT b.id, b.item_id, s.item_id
FROM boms b JOIN bom_line_substitutes s ON s.bom_id = b.id WHERE b.company_id = @company_id;

-- name: ListBomSubstitutes :many
SELECT s.line_no, s.item_id, s.ratio, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM bom_line_substitutes s JOIN items i ON i.id = s.item_id JOIN units u ON u.id = i.base_unit_id
WHERE s.bom_id = @bom_id ORDER BY s.line_no, i.code;

-- name: AddBomSubstitute :exec
INSERT INTO bom_line_substitutes (bom_id, line_no, item_id, ratio) VALUES (@bom_id, @line_no, @item_id, @ratio);

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
       COALESCE((SELECT SUM(c.qty) FROM work_order_completions c WHERE c.work_order_id = w.id AND c.status = 'posted'), 0)::numeric AS completed_qty,
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
                         processing_cost, output_lot_no, output_expiry, output_bin_code, due_date, note, created_by, updated_by)
VALUES (@company_id, @doc_no, @doc_date, @item_id, @plan_qty, @warehouse_id, @material_warehouse_id,
        @processing_cost, @output_lot_no, sqlc.narg(output_expiry), @output_bin_code, sqlc.narg(due_date), @note, @actor_id, @actor_id)
RETURNING *;

-- name: UpdateWorkOrderHeader :one
UPDATE work_orders SET doc_date = @doc_date, item_id = @item_id, plan_qty = @plan_qty, warehouse_id = @warehouse_id,
    material_warehouse_id = @material_warehouse_id, processing_cost = @processing_cost, output_lot_no = @output_lot_no,
    output_expiry = sqlc.narg(output_expiry), output_bin_code = @output_bin_code, due_date = sqlc.narg(due_date), note = @note,
    version = version + 1, updated_by = @actor_id
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: DeleteWorkOrderLines :exec
DELETE FROM work_order_lines WHERE work_order_id = @work_order_id;

-- name: AddWorkOrderLine :exec
INSERT INTO work_order_lines (work_order_id, line_no, item_id, qty, lot_no, bin_code, note)
VALUES (@work_order_id, @line_no, @item_id, @qty, @lot_no, @bin_code, @note);

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
SELECT x.output_item_id, x.material_id, SUM(x.qty)::numeric AS qty FROM (
    SELECT w.item_id AS output_item_id, t.item_id AS material_id, t.qty
    FROM inventory_transactions t JOIN work_orders w ON w.id = t.source_id
    WHERE t.company_id = @company_id AND t.source_type = 'work_order_issue'
      AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
    UNION ALL
    SELECT w.item_id, t.item_id, t.qty
    FROM inventory_transactions t
    JOIN work_order_completions c ON c.id = t.source_id JOIN work_orders w ON w.id = c.work_order_id
    WHERE t.company_id = @company_id AND t.source_type = 'wo_completion_issue'
      AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
    UNION ALL
    SELECT o.item_id, t.item_id, t.qty
    FROM inventory_transactions t
    JOIN subcontract_txns c ON c.id = t.source_id JOIN subcontract_orders o ON o.id = c.order_id
    WHERE t.company_id = @company_id AND t.source_type = 'sc_receive_issue'
      AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
) x
GROUP BY x.output_item_id, x.material_id
HAVING SUM(x.qty) <> 0;

-- name: ProductionProcessing :many
-- 期間內的加工費,依成品彙總:舊式一次完工的工單(已過帳、沒有完工回報)用工單加工費與工單日期,
-- 分批完工的用各完工回報(未作廢)的本次加工費與回報日期
SELECT x.item_id, SUM(x.processing)::numeric AS processing FROM (
    SELECT w.item_id, w.processing_cost AS processing
    FROM work_orders w
    WHERE w.company_id = @company_id AND w.status = 'posted'
      AND NOT EXISTS (SELECT 1 FROM work_order_completions c WHERE c.work_order_id = w.id)
      AND w.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
    UNION ALL
    SELECT w.item_id, c.processing_cost
    FROM work_order_completions c JOIN work_orders w ON w.id = c.work_order_id
    WHERE c.company_id = @company_id AND c.status = 'posted'
      AND c.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
    UNION ALL
    SELECT o.item_id, c.processing_fee
    FROM subcontract_txns c JOIN subcontract_orders o ON o.id = c.order_id
    WHERE c.company_id = @company_id AND c.kind = 'receive' AND c.status = 'posted'
      AND c.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
) x
GROUP BY x.item_id;

-- name: WritebackProduceCost :exec
-- 完工入庫的單位成本 = 該成品當月的完工成本 ÷ 完工數量
UPDATE inventory_transactions SET unit_cost = @unit_cost
WHERE company_id = @company_id AND item_id = @item_id AND source_type IN ('work_order_receipt', 'wo_completion_receipt', 'sc_receive_receipt')
  AND doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date;

-- name: ItemHasWorkOrders :one
SELECT EXISTS (SELECT 1 FROM work_orders WHERE company_id = @company_id AND item_id = @item_id);

-- ======== MRP 需求計算(D89) ========

-- name: MrpItems :many
-- 參與計算的料品:啟用中的商品類
SELECT i.id, i.code, i.name, u.name AS unit_name, i.safety_stock
FROM items i JOIN units u ON u.id = i.base_unit_id
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods';

-- name: MrpSalesDemand :many
-- 已核准訂單尚未出貨的量(基本單位)
SELECT l.item_id, COALESCE(o.delivery_date, @today::date)::date AS due,
       COALESCE(SUM(l.base_qty * (l.qty - dlv.delivered_qty) / l.qty), 0)::numeric AS qty
FROM sales_order_lines l
JOIN sales_orders o ON o.id = l.order_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(dl.qty), 0)::numeric AS delivered_qty FROM delivery_lines dl
    JOIN deliveries d ON d.id = dl.delivery_id
    WHERE dl.so_line_id = l.id AND d.status = 'posted'
) dlv
WHERE o.company_id = @company_id AND o.doc_type = 'order' AND o.status = 'approved' AND l.qty > dlv.delivered_qty
GROUP BY l.item_id, due;

-- name: MrpPurchaseSupply :many
-- 已核准採購單尚未進貨的量(基本單位)
SELECT l.item_id, COALESCE(o.expected_date, @today::date)::date AS due,
       COALESCE(SUM(l.base_qty * (l.qty - rec.received_qty) / l.qty), 0)::numeric AS qty
FROM purchase_order_lines l
JOIN purchase_orders o ON o.id = l.order_id
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(rl.qty), 0)::numeric AS received_qty FROM goods_receipt_lines rl
    JOIN goods_receipts r ON r.id = rl.receipt_id
    WHERE rl.po_line_id = l.id AND r.status = 'posted'
) rec
WHERE o.company_id = @company_id AND o.status = 'approved' AND l.qty > rec.received_qty
GROUP BY l.item_id, due;

-- name: MrpWorkOrderSupply :many
-- 尚未完工的工單(待審 / 已核准)與進行中的委外單預計入庫量(扣掉已分批完工 / 已收回的部分)
SELECT x.item_id, x.due, SUM(x.qty)::numeric AS qty FROM (
    SELECT w.item_id, COALESCE(w.due_date, w.doc_date)::date AS due, GREATEST(w.plan_qty - COALESCE(c.done, 0), 0) AS qty
    FROM work_orders w
    LEFT JOIN LATERAL (SELECT SUM(qty) AS done FROM work_order_completions WHERE work_order_id = w.id AND status = 'posted') c ON TRUE
    WHERE w.company_id = @company_id AND w.status IN ('pending', 'approved')
  UNION ALL
    SELECT o.item_id, COALESCE(o.due_date, o.doc_date)::date,
           GREATEST(o.plan_qty - COALESCE((SELECT SUM(t.qty) FROM subcontract_txns t WHERE t.order_id = o.id AND t.kind = 'receive' AND t.status = 'posted'), 0), 0)
    FROM subcontract_orders o WHERE o.company_id = @company_id AND o.status = 'open'
) x GROUP BY x.item_id, x.due;

-- name: MrpWorkOrderDemand :many
-- 尚未完工工單的領料量(依剩餘未完工比例),加上委外單還沒發給加工廠的材料量(依剩餘未收回比例,扣掉已在加工廠的)
SELECT x.item_id, x.due, SUM(x.qty)::numeric AS qty FROM (
    SELECT l.item_id, COALESCE(w.due_date, w.doc_date)::date AS due,
           l.qty * GREATEST(w.plan_qty - COALESCE(c.done, 0), 0) / w.plan_qty AS qty
    FROM work_order_lines l JOIN work_orders w ON w.id = l.work_order_id
    LEFT JOIN LATERAL (SELECT SUM(qty) AS done FROM work_order_completions WHERE work_order_id = w.id AND status = 'posted') c ON TRUE
    WHERE w.company_id = @company_id AND w.status IN ('pending', 'approved')
  UNION ALL
    SELECT l.item_id, COALESCE(o.due_date, o.doc_date)::date,
           GREATEST(l.qty * GREATEST(o.plan_qty - COALESCE((SELECT SUM(t.qty) FROM subcontract_txns t WHERE t.order_id = o.id AND t.kind = 'receive' AND t.status = 'posted'), 0), 0) / o.plan_qty
                    - COALESCE((SELECT SUM(x2.qty * CASE WHEN t.kind = 'issue' THEN 1 ELSE -1 END)
                                FROM subcontract_txn_lines x2 JOIN subcontract_txns t ON t.id = x2.txn_id
                                WHERE t.order_id = o.id AND t.status = 'posted' AND x2.item_id = l.item_id), 0), 0)
    FROM subcontract_order_lines l JOIN subcontract_orders o ON o.id = l.order_id
    WHERE o.company_id = @company_id AND o.status = 'open'
) x GROUP BY x.item_id, x.due;

-- name: MrpOnHand :many
SELECT item_id, SUM(qty)::numeric AS qty FROM inventory_balances WHERE company_id = @company_id GROUP BY item_id;

-- name: MrpActiveBoms :many
-- 每個成品今天適用的 BOM(已生效且啟用中,生效日最晚的一份)
SELECT DISTINCT ON (item_id) id, item_id, yield_qty FROM boms
WHERE company_id = @company_id AND is_active AND effective_from <= @on_date
ORDER BY item_id, effective_from DESC;

-- name: MrpSubstituteEdges :many
-- 指定 BOM 的替代料關係:主料 → 替代料(ratio = 1 單位主料需幾單位替代料),MRP 缺料時提示用
SELECT l.item_id AS main_id, s.item_id AS sub_id, s.ratio
FROM bom_line_substitutes s
JOIN bom_lines l ON l.bom_id = s.bom_id AND l.line_no = s.line_no
WHERE s.bom_id = ANY(@bom_ids::bigint[]);

-- ======== 工單分批完工(D94) ========

-- name: CreateCompletion :one
INSERT INTO work_order_completions (company_id, work_order_id, doc_no, doc_date, qty, processing_cost, output_lot_no, output_expiry,
                                    output_bin_code, note, created_by)
VALUES (@company_id, @work_order_id, @doc_no, @doc_date, @qty, @processing_cost, @output_lot_no, sqlc.narg(output_expiry),
        @output_bin_code, @note, @actor_id)
RETURNING *;

-- name: AddCompletionLine :exec
INSERT INTO work_order_completion_lines (completion_id, line_no, item_id, qty, lot_no, bin_code)
VALUES (@completion_id, @line_no, @item_id, @qty, @lot_no, @bin_code);

-- name: LockCompletion :one
SELECT * FROM work_order_completions WHERE id = @id AND work_order_id = @work_order_id AND company_id = @company_id FOR UPDATE;

-- name: VoidCompletion :exec
UPDATE work_order_completions SET status = 'voided', voided_by = @actor_id, voided_at = now() WHERE id = @id;

-- name: ListCompletions :many
SELECT c.*, cu.name AS created_by_name, vu.name AS voided_by_name
FROM work_order_completions c
LEFT JOIN users cu ON cu.id = c.created_by LEFT JOIN users vu ON vu.id = c.voided_by
WHERE c.work_order_id = @work_order_id ORDER BY c.id;

-- name: ListCompletionLines :many
SELECT l.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM work_order_completion_lines l JOIN items i ON i.id = l.item_id JOIN units u ON u.id = i.base_unit_id
WHERE l.completion_id = @completion_id ORDER BY l.line_no;

-- name: CompletedQty :one
-- 工單目前有效的累計完工量
SELECT COALESCE(SUM(qty), 0)::numeric FROM work_order_completions WHERE work_order_id = @work_order_id AND status = 'posted';

-- name: CountCompletionsOfOrder :one
-- 工單所有完工回報筆數(含已作廢):有任何紀錄就不能再用一次完工 / 反過帳
SELECT count(*) FROM work_order_completions WHERE work_order_id = @work_order_id;

-- name: ListCompletionLots :many
-- 某次完工回報實際入庫與領用的批號
SELECT t.source_type, t.item_id, l.lot_no, l.expiry_date, t.qty
FROM inventory_transactions t JOIN item_lots l ON l.id = t.lot_id
WHERE t.source_type IN ('wo_completion_issue', 'wo_completion_receipt') AND t.source_id = @completion_id
  AND t.reversal_of IS NULL AND NOT EXISTS (SELECT 1 FROM inventory_transactions r WHERE r.reversal_of = t.id)
ORDER BY t.id;

-- ======== MRP 計畫參數(D98) ========

-- name: MrpParams :many
SELECT item_id, lead_time_days, min_order_qty, order_multiple FROM item_mrp_params WHERE company_id = @company_id;

-- name: ListMrpParams :many
-- 啟用中的商品類料品與其 MRP 參數(沒設定的參數為 0)
SELECT i.id AS item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       COALESCE(p.lead_time_days, 0)::int AS lead_time_days,
       COALESCE(p.min_order_qty, 0)::numeric AS min_order_qty,
       COALESCE(p.order_multiple, 0)::numeric AS order_multiple,
       EXISTS (SELECT 1 FROM boms b WHERE b.item_id = i.id AND b.is_active) AS has_bom
FROM items i JOIN units u ON u.id = i.base_unit_id
LEFT JOIN item_mrp_params p ON p.item_id = i.id
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods'
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountMrpParams :one
SELECT count(*) FROM items i
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods'
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: UpsertMrpParams :exec
INSERT INTO item_mrp_params (item_id, company_id, lead_time_days, min_order_qty, order_multiple, updated_by)
VALUES (@item_id, @company_id, @lead_time_days, @min_order_qty, @order_multiple, @actor_id)
ON CONFLICT (item_id) DO UPDATE SET lead_time_days = EXCLUDED.lead_time_days, min_order_qty = EXCLUDED.min_order_qty,
    order_multiple = EXCLUDED.order_multiple, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: DeleteMrpParams :exec
DELETE FROM item_mrp_params WHERE item_id = @item_id AND company_id = @company_id;

-- name: MrpParamsItemOK :one
SELECT EXISTS (SELECT 1 FROM items WHERE id = @id AND company_id = @company_id AND item_type = 'goods');
