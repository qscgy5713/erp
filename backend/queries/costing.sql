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
       COALESCE(SUM(t.qty) FILTER (WHERE t.source_type NOT IN ('goods_receipt', 'purchase_return', 'delivery',
                                   'sales_return', 'stock_adjustment', 'stock_count', 'stock_transfer', 'opening_stock')), 0)::numeric AS other_qty
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
                        adjust_qty, avg_cost, cogs_amount, adjust_amount, closing_qty, closing_value)
VALUES (@closing_id, @item_id, @opening_qty, @opening_value, @purchase_qty, @purchase_value, @sales_qty,
        @adjust_qty, @avg_cost, @cogs_amount, @adjust_amount, @closing_qty, @closing_value);

-- name: DeleteCostClosing :exec
DELETE FROM cost_closings WHERE id = @id;

-- name: WritebackUnitCost :exec
-- 把平均成本回寫到沒有成本的流水帳(出貨、銷貨退回、調整、盤點、調撥);進貨的單位成本保留原值
UPDATE inventory_transactions SET unit_cost = @avg_cost
WHERE company_id = @company_id AND item_id = @item_id
  AND doc_date BETWEEN sqlc.arg(start_date)::date AND sqlc.arg(end_date)::date
  AND source_type NOT IN ('goods_receipt', 'purchase_return', 'opening_stock');

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
