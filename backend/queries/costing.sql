-- ======== 月結成本 ========

-- name: ListCostClosings :many
SELECT c.*, u.name AS closed_by_name FROM cost_closings c LEFT JOIN users u ON u.id = c.closed_by
WHERE c.company_id = @company_id ORDER BY c.period DESC;

-- name: GetCostClosing :one
SELECT * FROM cost_closings WHERE company_id = @company_id AND period = @period::char(7);

-- name: CostClosingExists :one
-- 該日期所屬月份是否已月結成本(已月結的月份不可再異動庫存)
SELECT EXISTS (SELECT 1 FROM cost_closings WHERE company_id = @company_id AND period = @period::char(7));

-- name: LatestCostClosingBefore :one
SELECT * FROM cost_closings WHERE company_id = @company_id AND period < @period::char(7)
ORDER BY period DESC LIMIT 1;

-- name: LatestCostClosing :one
SELECT * FROM cost_closings WHERE company_id = @company_id ORDER BY period DESC LIMIT 1;

-- name: UncostedEarlierMonth :one
-- 在指定月份之前、有庫存異動卻尚未月結的最早月份(月結須依序進行)
SELECT x.m::text AS month FROM (
    SELECT DISTINCT to_char(it.doc_date, 'YYYY-MM') AS m FROM inventory_transactions it
    WHERE it.company_id = @company_id AND it.doc_date < @start_date::date
) x
WHERE NOT EXISTS (SELECT 1 FROM cost_closings c WHERE c.company_id = @company_id AND c.period = x.m)
ORDER BY x.m LIMIT 1;

-- name: CostPeriodAggregates :many
-- 期間內各料品的異動彙總(基本單位);其他來源(未知類型)併入 other_qty,以平均成本計價
SELECT t.item_id,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type IN ('goods_receipt', 'purchase_return', 'opening_stock')), 0)::numeric AS purchase_qty,
       COALESCE(SUM(t.qty * COALESCE(t.unit_cost, 0)) FILTER (WHERE t.source_type IN ('goods_receipt', 'purchase_return', 'opening_stock')), 0)::numeric AS purchase_value,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type IN ('delivery', 'sales_return')), 0)::numeric AS sales_qty,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type IN ('stock_adjustment', 'stock_count')), 0)::numeric AS adjust_qty,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type IN ('work_order_issue', 'wo_completion_issue', 'sc_receive_issue')), 0)::numeric AS consume_qty,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type IN ('work_order_receipt', 'wo_completion_receipt', 'sc_receive_receipt')), 0)::numeric AS produce_qty,
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type NOT IN ('goods_receipt', 'purchase_return', 'delivery',
                                   'sales_return', 'stock_adjustment', 'stock_count', 'stock_transfer', 'opening_stock',
                                   'work_order_issue', 'work_order_receipt', 'wo_completion_issue', 'wo_completion_receipt',
                                   'sc_issue', 'sc_return', 'sc_receive_issue', 'sc_receive_receipt', 'wo_wip_issue', 'wo_wip_return')), 0)::numeric AS other_qty
FROM inventory_transactions t
WHERE t.company_id = @company_id AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
GROUP BY t.item_id
ORDER BY t.item_id;

-- name: ListItemCostsForClosing :many
SELECT ic.*, i.code AS item_code, i.name AS item_name, u.name AS unit_name
FROM item_costs ic JOIN items i ON i.id = ic.item_id JOIN units u ON u.id = i.base_unit_id
WHERE ic.closing_id = @closing_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountItemCostsForClosing :one
SELECT count(*) FROM item_costs ic JOIN items i ON i.id = ic.item_id
WHERE ic.closing_id = @closing_id
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%');

-- name: ListItemCostsRaw :many
SELECT * FROM item_costs WHERE closing_id = @closing_id;

-- name: InsertCostClosing :one
INSERT INTO cost_closings (company_id, period, item_count, cogs_amount, adjust_amount, inventory_value, closed_by)
VALUES (@company_id, @period::char(7), @item_count, @cogs_amount, @adjust_amount, @inventory_value, @closed_by)
RETURNING *;

-- name: InsertItemCost :exec
INSERT INTO item_costs (closing_id, item_id, opening_qty, opening_value, purchase_qty, purchase_value, sales_qty,
                        adjust_qty, avg_cost, cogs_amount, adjust_amount, closing_qty, closing_value,
                        consume_qty, consume_value)
VALUES (@closing_id, @item_id, @opening_qty, @opening_value, @purchase_qty, @purchase_value, @sales_qty,
        @adjust_qty, @avg_cost, @cogs_amount, @adjust_amount, @closing_qty, @closing_value,
        @consume_qty, @consume_value);

-- name: DeleteCostClosing :exec
DELETE FROM cost_closings WHERE id = @id;

-- name: WritebackUnitCost :exec
-- 把平均成本回寫到沒有成本的流水帳(出貨、銷貨退回、調整、盤點、調撥);進貨的單位成本保留原值
UPDATE inventory_transactions SET unit_cost = @avg_cost
WHERE company_id = @company_id AND item_id = @item_id
  AND doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
  AND source_type NOT IN ('goods_receipt', 'purchase_return', 'opening_stock', 'work_order_receipt', 'wo_completion_receipt', 'sc_receive_receipt');

-- ======== 對帳檢查 ========

-- name: GLBalanceAsOf :one
-- 科目截至某日(含)已過帳傳票的借方 − 貸方
SELECT COALESCE(SUM(l.debit - l.credit), 0)::numeric AS balance
FROM voucher_lines l JOIN vouchers v ON v.id = l.voucher_id
WHERE v.company_id = @company_id AND v.status = 'posted' AND l.account_id = @account_id
  AND v.voucher_date <= sqlc.arg(as_of)::date;

-- name: ReceivableBaseBalance :one
SELECT COALESCE(SUM(CASE WHEN amount = 0 THEN 0 ELSE base_amount * (amount - paid_amount) / amount END), 0)::numeric AS balance
FROM accounts_receivable WHERE company_id = @company_id;

-- name: PayableBaseBalance :one
SELECT COALESCE(SUM(CASE WHEN amount = 0 THEN 0 ELSE base_amount * (amount - paid_amount) / amount END), 0)::numeric AS balance
FROM accounts_payable WHERE company_id = @company_id;

-- name: InventoryBalanceMismatches :one
-- 現有量與流水帳合計不一致的(料品, 倉庫)筆數,應為 0
SELECT count(*) FROM (
    SELECT b.item_id, b.warehouse_id FROM inventory_balances b
    LEFT JOIN (SELECT it.item_id, it.warehouse_id, SUM(it.qty) AS q FROM inventory_transactions it
               WHERE it.company_id = @company_id GROUP BY it.item_id, it.warehouse_id) t
           ON t.item_id = b.item_id AND t.warehouse_id = b.warehouse_id
    WHERE b.company_id = @company_id AND b.qty <> COALESCE(t.q, 0)
) x;

-- name: UnbalancedVouchers :one
SELECT count(*) FROM (
    SELECT v.id FROM vouchers v JOIN voucher_lines l ON l.voucher_id = v.id
    WHERE v.company_id = @company_id AND v.status = 'posted'
    GROUP BY v.id HAVING SUM(l.debit) <> SUM(l.credit)
) x;

-- name: UncostedStockMonths :one
-- 已有庫存異動、但尚未月結成本的月份數(不含本月)
SELECT count(*) FROM (
    SELECT DISTINCT to_char(it.doc_date, 'YYYY-MM') AS m FROM inventory_transactions it
    WHERE it.company_id = @company_id AND it.doc_date < sqlc.arg(month_start)::date
) x WHERE NOT EXISTS (SELECT 1 FROM cost_closings c WHERE c.company_id = @company_id AND c.period = x.m);

-- name: LotBalanceMismatches :one
-- 批號現有量的完整性:①各批號現有量 = 流水帳該批號的合計;②批號管理料品各批號合計 = 料品現有量。筆數應為 0
SELECT (
    (SELECT count(*) FROM (
        SELECT b.lot_id, b.warehouse_id FROM inventory_lot_balances b
        LEFT JOIN (SELECT t.lot_id, t.warehouse_id, SUM(t.qty) AS q FROM inventory_transactions t
                   WHERE t.company_id = @company_id AND t.lot_id IS NOT NULL GROUP BY t.lot_id, t.warehouse_id) x
               ON x.lot_id = b.lot_id AND x.warehouse_id = b.warehouse_id
        WHERE b.company_id = @company_id AND b.qty <> COALESCE(x.q, 0)
    ) a)
    +
    (SELECT count(*) FROM (
        SELECT ib.item_id, ib.warehouse_id FROM inventory_balances ib JOIN items i ON i.id = ib.item_id
        LEFT JOIN (SELECT lb.item_id, lb.warehouse_id, SUM(lb.qty) AS q FROM inventory_lot_balances lb
                   WHERE lb.company_id = @company_id GROUP BY lb.item_id, lb.warehouse_id) y
               ON y.item_id = ib.item_id AND y.warehouse_id = ib.warehouse_id
        WHERE ib.company_id = @company_id AND i.lot_control <> 'none' AND ib.qty <> COALESCE(y.q, 0)
    ) c)
)::bigint AS mismatches;

-- ======== 標準成本與差異(D91) ========

-- name: ListStandardCosts :many
-- 啟用中的商品類料品與其標準成本(沒設定的 standard_cost 為 NULL);has_bom 表示有 BOM(通常是要設標準成本的成品)
SELECT i.id AS item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       sc.standard_cost, sc.updated_at,
       EXISTS (SELECT 1 FROM boms b WHERE b.item_id = i.id AND b.is_active) AS has_bom
FROM items i JOIN units u ON u.id = i.base_unit_id
LEFT JOIN item_standard_costs sc ON sc.item_id = i.id AND sc.effective_from = (
    SELECT max(s2.effective_from) FROM item_standard_costs s2 WHERE s2.item_id = i.id AND s2.effective_from <= CURRENT_DATE)
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods'
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (NOT @only_bom::boolean OR EXISTS (SELECT 1 FROM boms b WHERE b.item_id = i.id AND b.is_active))
ORDER BY i.code
LIMIT @lim OFFSET @off;

-- name: CountStandardCosts :one
SELECT count(*) FROM items i
WHERE i.company_id = @company_id AND i.is_active AND i.item_type = 'goods'
  AND (sqlc.narg(keyword)::text IS NULL OR i.code ILIKE '%' || sqlc.narg(keyword) || '%' OR i.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (NOT @only_bom::boolean OR EXISTS (SELECT 1 FROM boms b WHERE b.item_id = i.id AND b.is_active));

-- name: UpsertStandardCost :exec
INSERT INTO item_standard_costs (item_id, company_id, effective_from, standard_cost, updated_by)
VALUES (@item_id, @company_id, @effective_from, @standard_cost, @actor_id)
ON CONFLICT (item_id, effective_from) DO UPDATE SET standard_cost = EXCLUDED.standard_cost, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: DeleteStandardCost :exec
-- 清除某個料品的全部標準成本版本
DELETE FROM item_standard_costs WHERE item_id = @item_id AND company_id = @company_id;

-- name: DeleteStandardCostVersion :execrows
DELETE FROM item_standard_costs WHERE item_id = @item_id AND company_id = @company_id AND effective_from = @effective_from;

-- name: ListStandardCostVersions :many
SELECT s.effective_from, s.standard_cost, s.updated_at, COALESCE(u.name, '')::text AS updated_by_name
FROM item_standard_costs s LEFT JOIN users u ON u.id = s.updated_by
WHERE s.item_id = @item_id AND s.company_id = @company_id ORDER BY s.effective_from DESC;

-- name: StandardCostItemOK :one
SELECT EXISTS (SELECT 1 FROM items WHERE id = @id AND company_id = @company_id AND item_type = 'goods');

-- name: StandardCostVariance :many
-- 期間內完工入庫的成品:數量、實際成本(月結寫回的單位成本 × 數量)與標準成本(依各筆完工日當時有效的版本);
-- uncosted_qty > 0 表示該月尚未月結;no_std_qty > 0 表示有完工量找不到當時有效的標準成本
SELECT t.item_id, i.code AS item_code, i.name AS item_name, u.name AS unit_name,
       SUM(t.qty)::numeric AS produced_qty,
       COALESCE(SUM(t.qty * t.unit_cost) FILTER (WHERE t.unit_cost IS NOT NULL), 0)::numeric AS actual_amount,
       COALESCE(SUM(t.qty) FILTER (WHERE t.unit_cost IS NULL), 0)::numeric AS uncosted_qty,
       COALESCE(SUM(t.qty * sc.standard_cost) FILTER (WHERE sc.standard_cost IS NOT NULL), 0)::numeric AS standard_amount,
       COALESCE(SUM(t.qty) FILTER (WHERE sc.standard_cost IS NULL), 0)::numeric AS no_std_qty
FROM inventory_transactions t
JOIN items i ON i.id = t.item_id JOIN units u ON u.id = i.base_unit_id
LEFT JOIN LATERAL (
    SELECT s.standard_cost FROM item_standard_costs s
    WHERE s.item_id = t.item_id AND s.effective_from <= t.doc_date ORDER BY s.effective_from DESC LIMIT 1
) sc ON TRUE
WHERE t.company_id = @company_id AND t.source_type IN ('work_order_receipt', 'wo_completion_receipt', 'sc_receive_receipt')
  AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
GROUP BY t.item_id, i.code, i.name, u.name
HAVING SUM(t.qty) <> 0
ORDER BY i.code;

-- ======== 成本差異拆解(D97) ========

-- name: VarianceBomLines :many
-- 各成品在期間結束日適用的 BOM(已生效且啟用中、生效日最晚的一份)的材料明細,用來算標準用量
SELECT b.item_id AS output_item_id, b.yield_qty, l.item_id AS material_id, l.qty, l.scrap_pct,
       mi.code AS material_code, mi.name AS material_name, u.name AS unit_name
FROM boms b
JOIN bom_lines l ON l.bom_id = b.id
JOIN items mi ON mi.id = l.item_id JOIN units u ON u.id = mi.base_unit_id
WHERE b.company_id = @company_id AND b.item_id = ANY(@item_ids::bigint[]) AND b.is_active
  AND b.effective_from = (
      SELECT max(b2.effective_from) FROM boms b2
      WHERE b2.company_id = b.company_id AND b2.item_id = b.item_id AND b2.is_active
        AND b2.effective_from <= sqlc.arg(end_date)::date)
ORDER BY b.item_id, l.line_no;

-- name: VarianceActualMaterials :many
-- 期間內工單實際領用的材料(含沖銷,淨額):數量(正數)與金額(月結寫回的材料平均成本 × 數量)
SELECT x.output_item_id, x.material_id, mi.code AS material_code, mi.name AS material_name, u.name AS unit_name,
       (-SUM(x.qty))::numeric AS qty,
       COALESCE(-SUM(x.qty * x.unit_cost) FILTER (WHERE x.unit_cost IS NOT NULL), 0)::numeric AS amount,
       COALESCE(-SUM(x.qty) FILTER (WHERE x.unit_cost IS NULL), 0)::numeric AS uncosted_qty
FROM (
    SELECT w.item_id AS output_item_id, t.item_id AS material_id, t.qty, t.unit_cost
    FROM inventory_transactions t JOIN work_orders w ON w.id = t.source_id
    WHERE t.company_id = @company_id AND t.source_type = 'work_order_issue'
      AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
  UNION ALL
    SELECT w.item_id, t.item_id, t.qty, t.unit_cost
    FROM inventory_transactions t
    JOIN work_order_completions c ON c.id = t.source_id JOIN work_orders w ON w.id = c.work_order_id
    WHERE t.company_id = @company_id AND t.source_type = 'wo_completion_issue'
  UNION ALL
    SELECT o.item_id, t.item_id, t.qty, t.unit_cost
    FROM inventory_transactions t
    JOIN subcontract_txns c ON c.id = t.source_id JOIN subcontract_orders o ON o.id = c.order_id
    WHERE t.company_id = @company_id AND t.source_type = 'sc_receive_issue'
      AND t.doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
) x
JOIN items mi ON mi.id = x.material_id JOIN units u ON u.id = mi.base_unit_id
WHERE x.output_item_id = ANY(@item_ids::bigint[])
GROUP BY x.output_item_id, x.material_id, mi.code, mi.name, u.name
HAVING SUM(x.qty) <> 0
ORDER BY x.output_item_id, mi.code;

-- name: StandardCostsOf :many
-- 各料品在指定日期當時有效的標準成本
SELECT DISTINCT ON (item_id) item_id, standard_cost FROM item_standard_costs
WHERE company_id = @company_id AND item_id = ANY(@item_ids::bigint[]) AND effective_from <= @as_of::date
ORDER BY item_id, effective_from DESC;
