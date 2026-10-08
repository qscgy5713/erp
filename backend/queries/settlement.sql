-- ======== 收款單 / 付款單 ========
-- scope_* 為資料範圍:收款單依負責業務快照過濾(D38);付款單無資料範圍

-- name: ListSettlements :many
SELECT s.id, s.side, s.doc_no, s.doc_date, s.currency, s.method, s.reference, s.amount, s.status, s.note,
       s.version, s.updated_at,
       COALESCE(c.code, sp.code) AS partner_code, COALESCE(c.name, sp.name) AS partner_name,
       su.name AS sales_user_name, cu.name AS created_by_name
FROM settlements s
LEFT JOIN customers c ON c.id = s.customer_id
LEFT JOIN suppliers sp ON sp.id = s.supplier_id
LEFT JOIN users su ON su.id = s.sales_user_id
LEFT JOIN users cu ON cu.id = s.created_by
WHERE s.company_id = @company_id AND s.side = @side::text
  AND (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status))
  AND (sqlc.narg(partner_id)::bigint IS NULL OR s.customer_id = sqlc.narg(partner_id) OR s.supplier_id = sqlc.narg(partner_id))
  AND (sqlc.narg(keyword)::text IS NULL OR s.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR s.reference ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR s.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR s.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR s.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
ORDER BY s.doc_date DESC, s.id DESC
LIMIT @lim OFFSET @off;

-- name: CountSettlements :one
SELECT count(*) FROM settlements s
LEFT JOIN users su ON su.id = s.sales_user_id
WHERE s.company_id = @company_id AND s.side = @side::text
  AND (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status))
  AND (sqlc.narg(partner_id)::bigint IS NULL OR s.customer_id = sqlc.narg(partner_id) OR s.supplier_id = sqlc.narg(partner_id))
  AND (sqlc.narg(keyword)::text IS NULL OR s.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR s.reference ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR s.doc_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR s.doc_date <= sqlc.narg(to_date))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR s.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id));

-- name: GetSettlement :one
SELECT s.*, COALESCE(c.code, sp.code) AS partner_code, COALESCE(c.name, sp.name) AS partner_name,
       su.name AS sales_user_name, su.department_id AS sales_department_id,
       cu.name AS created_by_name, sbu.name AS submitted_by_name, au.name AS approved_by_name,
       pu.name AS posted_by_name
FROM settlements s
LEFT JOIN customers c ON c.id = s.customer_id
LEFT JOIN suppliers sp ON sp.id = s.supplier_id
LEFT JOIN users su ON su.id = s.sales_user_id
LEFT JOIN users cu ON cu.id = s.created_by
LEFT JOIN users sbu ON sbu.id = s.submitted_by
LEFT JOIN users au ON au.id = s.approved_by
LEFT JOIN users pu ON pu.id = s.posted_by
WHERE s.id = @id AND s.company_id = @company_id;

-- name: LockSettlement :one
SELECT * FROM settlements WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: CreateSettlement :one
INSERT INTO settlements (company_id, side, doc_no, doc_date, customer_id, supplier_id, sales_user_id, currency,
                         method, reference, amount, note, created_by, updated_by)
VALUES (@company_id, @side, @doc_no, @doc_date, sqlc.narg(customer_id), sqlc.narg(supplier_id),
        sqlc.narg(sales_user_id), @currency, @method, @reference, @amount, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateSettlementHeader :one
UPDATE settlements
SET doc_date = @doc_date, customer_id = sqlc.narg(customer_id), supplier_id = sqlc.narg(supplier_id),
    sales_user_id = sqlc.narg(sales_user_id), currency = @currency, method = @method, reference = @reference,
    amount = @amount, note = @note, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetSettlementStatus :one
UPDATE settlements
SET status       = @status,
    submitted_by = CASE WHEN @action::text = 'submit' THEN @actor_id::bigint ELSE submitted_by END,
    submitted_at = CASE WHEN @action::text = 'submit' THEN now() ELSE submitted_at END,
    approved_by  = CASE WHEN @action::text = 'approve' THEN @actor_id::bigint
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_by END,
    approved_at  = CASE WHEN @action::text = 'approve' THEN now()
                        WHEN @action::text IN ('reject', 'unapprove') THEN NULL ELSE approved_at END,
    posted_by    = CASE WHEN @action::text = 'post' THEN @actor_id::bigint
                        WHEN @action::text = 'unpost' THEN NULL ELSE posted_by END,
    posted_at    = CASE WHEN @action::text = 'post' THEN now()
                        WHEN @action::text = 'unpost' THEN NULL ELSE posted_at END,
    version      = version + 1,
    updated_by   = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListSettlementLines :many
SELECT l.id, l.line_no, l.receivable_id, l.payable_id, l.amount,
       COALESCE(r.source_no, p.source_no) AS source_no, COALESCE(r.source_type, p.source_type) AS source_type,
       COALESCE(r.doc_date, p.doc_date) AS source_date, COALESCE(r.due_date, p.due_date) AS due_date,
       COALESCE(r.amount, p.amount) AS source_amount, COALESCE(r.paid_amount, p.paid_amount) AS source_paid
FROM settlement_lines l
LEFT JOIN accounts_receivable r ON r.id = l.receivable_id
LEFT JOIN accounts_payable p ON p.id = l.payable_id
WHERE l.settlement_id = @settlement_id
ORDER BY l.line_no;

-- name: DeleteSettlementLines :exec
DELETE FROM settlement_lines WHERE settlement_id = @settlement_id;

-- name: AddSettlementLine :exec
INSERT INTO settlement_lines (settlement_id, line_no, receivable_id, payable_id, amount)
VALUES (@settlement_id, @line_no, sqlc.narg(receivable_id), sqlc.narg(payable_id), @amount);

-- name: LockReceivables :many
-- 過帳 / 反過帳時依 id 順序鎖定要沖的應收;同時用於開單驗證(不鎖也可讀)
SELECT id, customer_id, currency, amount, base_amount, paid_amount, source_no FROM accounts_receivable
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: LockPayables :many
SELECT id, supplier_id, currency, amount, base_amount, paid_amount, source_no FROM accounts_payable
WHERE company_id = @company_id AND id = ANY(@ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: AddReceivablePaid :exec
UPDATE accounts_receivable SET paid_amount = paid_amount + @delta WHERE id = @id;

-- name: AddPayablePaid :exec
UPDATE accounts_payable SET paid_amount = paid_amount + @delta WHERE id = @id;

-- name: ReceivableSettlementNo :one
-- 應收是否已被未作廢的收款單引用(來源單據反過帳前檢查)
SELECT s.doc_no FROM settlement_lines l JOIN settlements s ON s.id = l.settlement_id
WHERE l.receivable_id = @receivable_id AND s.status <> 'voided' LIMIT 1;

-- name: PayableSettlementNo :one
SELECT s.doc_no FROM settlement_lines l JOIN settlements s ON s.id = l.settlement_id
WHERE l.payable_id = @payable_id AND s.status <> 'voided' LIMIT 1;

-- ======== 對帳單 ========
-- 以單一幣別計算;應收 / 應付的異動 = 帳款金額(退回 / 退出為負),已過帳的收付款為反向

-- name: ReceivableStatementOpening :one
SELECT (COALESCE((SELECT SUM(r.amount) FROM accounts_receivable r
                  WHERE r.company_id = @company_id AND r.customer_id = @partner_id AND r.currency = @currency::text
                    AND r.doc_date < sqlc.arg(from_date)::date), 0)
      - COALESCE((SELECT SUM(l.amount) FROM settlement_lines l JOIN settlements s ON s.id = l.settlement_id
                  WHERE s.company_id = @company_id AND s.customer_id = @partner_id AND s.currency = @currency::text
                    AND s.status = 'posted' AND s.doc_date < sqlc.arg(from_date)::date), 0))::numeric AS opening;

-- name: ReceivableStatement :many
SELECT txn_date, kind, doc_no, delta, ref_id FROM (
    SELECT r.doc_date AS txn_date, r.source_type::text AS kind, r.source_no AS doc_no, r.amount AS delta,
           r.source_id AS ref_id, 0 AS ord
    FROM accounts_receivable r
    WHERE r.company_id = @company_id AND r.customer_id = @partner_id AND r.currency = @currency::text
      AND r.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
    UNION ALL
    SELECT s.doc_date, 'receipt'::text, s.doc_no, -SUM(l.amount), s.id, 1
    FROM settlements s JOIN settlement_lines l ON l.settlement_id = s.id
    WHERE s.company_id = @company_id AND s.customer_id = @partner_id AND s.currency = @currency::text
      AND s.status = 'posted' AND s.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
    GROUP BY s.id
) t ORDER BY txn_date, ord, doc_no;

-- name: PayableStatementOpening :one
SELECT (COALESCE((SELECT SUM(p.amount) FROM accounts_payable p
                  WHERE p.company_id = @company_id AND p.supplier_id = @partner_id AND p.currency = @currency::text
                    AND p.doc_date < sqlc.arg(from_date)::date), 0)
      - COALESCE((SELECT SUM(l.amount) FROM settlement_lines l JOIN settlements s ON s.id = l.settlement_id
                  WHERE s.company_id = @company_id AND s.supplier_id = @partner_id AND s.currency = @currency::text
                    AND s.status = 'posted' AND s.doc_date < sqlc.arg(from_date)::date), 0))::numeric AS opening;

-- name: PayableStatement :many
SELECT txn_date, kind, doc_no, delta, ref_id FROM (
    SELECT p.doc_date AS txn_date, p.source_type::text AS kind, p.source_no AS doc_no, p.amount AS delta,
           p.source_id AS ref_id, 0 AS ord
    FROM accounts_payable p
    WHERE p.company_id = @company_id AND p.supplier_id = @partner_id AND p.currency = @currency::text
      AND p.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
    UNION ALL
    SELECT s.doc_date, 'payment'::text, s.doc_no, -SUM(l.amount), s.id, 1
    FROM settlements s JOIN settlement_lines l ON l.settlement_id = s.id
    WHERE s.company_id = @company_id AND s.supplier_id = @partner_id AND s.currency = @currency::text
      AND s.status = 'posted' AND s.doc_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
    GROUP BY s.id
) t ORDER BY txn_date, ord, doc_no;

-- ======== 帳齡分析 ========
-- 以目前未沖餘額(本位幣,依原幣餘額比例換算)按到期日分組;as_of 之後的單據不計

-- name: ReceivableAging :many
WITH items AS (
    SELECT r.customer_id AS partner_id, (sqlc.arg(as_of)::date - r.due_date)::int AS days,
           (CASE WHEN r.amount = 0 THEN 0 ELSE r.base_amount * (r.amount - r.paid_amount) / r.amount END)::numeric AS bal
    FROM accounts_receivable r
    WHERE r.company_id = @company_id AND r.amount <> r.paid_amount AND r.doc_date <= sqlc.arg(as_of)::date
)
SELECT c.id AS partner_id, c.code AS partner_code, c.name AS partner_name,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days <= 0), 0)::numeric AS not_due,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 1 AND 30), 0)::numeric AS d1_30,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 31 AND 60), 0)::numeric AS d31_60,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 61 AND 90), 0)::numeric AS d61_90,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days > 90), 0)::numeric AS d90_plus,
       COALESCE(SUM(i.bal), 0)::numeric AS total
FROM items i
JOIN customers c ON c.id = i.partner_id
LEFT JOIN users su ON su.id = c.sales_user_id
WHERE (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
GROUP BY c.id
ORDER BY c.code;

-- name: PayableAging :many
WITH items AS (
    SELECT p.supplier_id AS partner_id, (sqlc.arg(as_of)::date - p.due_date)::int AS days,
           (CASE WHEN p.amount = 0 THEN 0 ELSE p.base_amount * (p.amount - p.paid_amount) / p.amount END)::numeric AS bal
    FROM accounts_payable p
    WHERE p.company_id = @company_id AND p.amount <> p.paid_amount AND p.doc_date <= sqlc.arg(as_of)::date
)
SELECT sp.id AS partner_id, sp.code AS partner_code, sp.name AS partner_name,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days <= 0), 0)::numeric AS not_due,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 1 AND 30), 0)::numeric AS d1_30,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 31 AND 60), 0)::numeric AS d31_60,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days BETWEEN 61 AND 90), 0)::numeric AS d61_90,
       COALESCE(SUM(i.bal) FILTER (WHERE i.days > 90), 0)::numeric AS d90_plus,
       COALESCE(SUM(i.bal), 0)::numeric AS total
FROM items i
JOIN suppliers sp ON sp.id = i.partner_id
GROUP BY sp.id
ORDER BY sp.code;

-- name: DeleteSettlementLinesByReceivable :exec
-- 移除應收前清掉已作廢收款單的明細(未作廢者已在呼叫端擋下)
DELETE FROM settlement_lines WHERE receivable_id = @receivable_id;

-- name: DeleteSettlementLinesByPayable :exec
DELETE FROM settlement_lines WHERE payable_id = @payable_id;
