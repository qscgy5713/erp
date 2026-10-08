-- ======== 會計科目 ========

-- name: ListAccounts :many
SELECT a.*, p.code AS parent_code,
       EXISTS (SELECT 1 FROM voucher_lines l WHERE l.account_id = a.id) AS has_entries
FROM accounts a
LEFT JOIN accounts p ON p.id = a.parent_id
WHERE a.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL OR a.code ILIKE sqlc.narg(keyword) || '%' OR a.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(is_active)::boolean IS NULL OR a.is_active = sqlc.narg(is_active))
  AND (sqlc.narg(postable_only)::boolean IS NULL OR NOT sqlc.narg(postable_only)::boolean OR a.is_postable)
ORDER BY a.code;

-- name: GetAccount :one
SELECT a.*, p.code AS parent_code,
       EXISTS (SELECT 1 FROM voucher_lines l WHERE l.account_id = a.id) AS has_entries
FROM accounts a
LEFT JOIN accounts p ON p.id = a.parent_id
WHERE a.id = @id AND a.company_id = @company_id;

-- name: CreateAccount :one
INSERT INTO accounts (company_id, code, name, acct_type, parent_id, is_postable, note, created_by, updated_by)
VALUES (@company_id, @code, @name, @acct_type, sqlc.narg(parent_id), @is_postable, @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateAccount :one
UPDATE accounts
SET name = @name, acct_type = @acct_type, parent_id = sqlc.narg(parent_id), is_postable = @is_postable,
    is_active = @is_active, note = @note, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: AccountParentCycle :one
-- 設定 parent 後是否會形成循環(parent 的祖先鏈中包含自己)
WITH RECURSIVE chain (cid) AS (
    SELECT sqlc.arg(parent_id)::bigint
    UNION ALL
    SELECT accounts.parent_id FROM accounts, chain WHERE accounts.id = chain.cid AND accounts.parent_id IS NOT NULL
)
SELECT EXISTS (SELECT 1 FROM chain WHERE chain.cid = sqlc.arg(self_id)::bigint);

-- name: AccountsForPosting :many
-- 記帳驗證:科目須屬同公司、啟用且為明細科目
SELECT id, code, name, is_active, is_postable FROM accounts
WHERE company_id = @company_id AND id = ANY(@ids::bigint[]);

-- ======== 拋轉規則 ========

-- name: ListAccountMappings :many
SELECT m.map_key, m.account_id, a.code AS account_code, a.name AS account_name, a.is_active, a.is_postable, m.updated_at
FROM account_mappings m JOIN accounts a ON a.id = m.account_id
WHERE m.company_id = @company_id
ORDER BY m.map_key;

-- name: SetAccountMapping :exec
INSERT INTO account_mappings (company_id, map_key, account_id, updated_by)
VALUES (@company_id, @map_key, @account_id, @updated_by)
ON CONFLICT (company_id, map_key) DO UPDATE SET account_id = EXCLUDED.account_id,
    updated_by = EXCLUDED.updated_by, updated_at = now();

-- ======== 會計期間 ========

-- name: GetPeriod :one
SELECT * FROM accounting_periods WHERE company_id = @company_id AND period = @period::char(7);

-- name: ListPeriods :many
SELECT p.period, p.status, p.closed_at, u.name AS closed_by_name,
       (SELECT count(*) FROM vouchers v WHERE v.company_id = p.company_id AND v.status = 'draft'
          AND to_char(v.voucher_date, 'YYYY-MM') = p.period) AS draft_vouchers
FROM accounting_periods p LEFT JOIN users u ON u.id = p.closed_by
WHERE p.company_id = @company_id
ORDER BY p.period DESC;

-- name: SetPeriodStatus :exec
INSERT INTO accounting_periods (company_id, period, status, closed_by, closed_at)
VALUES (@company_id, @period::char(7), @status::text, sqlc.narg(actor_id), CASE WHEN @status::text = 'closed' THEN now() END)
ON CONFLICT (company_id, period) DO UPDATE
SET status = EXCLUDED.status, closed_by = EXCLUDED.closed_by, closed_at = EXCLUDED.closed_at, updated_at = now();

-- name: CountDraftVouchersInPeriod :one
SELECT count(*) FROM vouchers
WHERE company_id = @company_id AND status = 'draft' AND to_char(voucher_date, 'YYYY-MM') = @period::text;

-- name: LockCompanyForPeriod :exec
-- 關帳 / 重開 / 過帳檢查期間時序列化(避免檢查與關帳交錯)
SELECT id FROM companies WHERE id = @company_id FOR SHARE;

-- name: LockCompanyForPeriodChange :exec
SELECT id FROM companies WHERE id = @company_id FOR UPDATE;

-- ======== 傳票 ========

-- name: ListVouchers :many
SELECT v.id, v.doc_no, v.voucher_date, v.source_type, v.source_id, v.source_no, v.description, v.status,
       v.reversal_of, v.total_amount, v.version, v.updated_at, cu.name AS created_by_name,
       EXISTS (SELECT 1 FROM vouchers r WHERE r.reversal_of = v.id) AS reversed
FROM vouchers v LEFT JOIN users cu ON cu.id = v.created_by
WHERE v.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR v.status = sqlc.narg(status))
  AND (sqlc.narg(source)::text IS NULL OR (sqlc.narg(source) = 'manual' AND v.source_type = 'manual')
       OR (sqlc.narg(source) = 'auto' AND v.source_type <> 'manual'))
  AND (sqlc.narg(keyword)::text IS NULL OR v.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR v.source_no ILIKE '%' || sqlc.narg(keyword) || '%' OR v.description ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR v.voucher_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR v.voucher_date <= sqlc.narg(to_date))
  AND (sqlc.narg(account_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM voucher_lines l WHERE l.voucher_id = v.id AND l.account_id = sqlc.narg(account_id)))
ORDER BY v.voucher_date DESC, v.id DESC
LIMIT @lim OFFSET @off;

-- name: CountVouchers :one
SELECT count(*) FROM vouchers v
WHERE v.company_id = @company_id
  AND (sqlc.narg(status)::text IS NULL OR v.status = sqlc.narg(status))
  AND (sqlc.narg(source)::text IS NULL OR (sqlc.narg(source) = 'manual' AND v.source_type = 'manual')
       OR (sqlc.narg(source) = 'auto' AND v.source_type <> 'manual'))
  AND (sqlc.narg(keyword)::text IS NULL OR v.doc_no ILIKE '%' || sqlc.narg(keyword) || '%'
       OR v.source_no ILIKE '%' || sqlc.narg(keyword) || '%' OR v.description ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(from_date)::date IS NULL OR v.voucher_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR v.voucher_date <= sqlc.narg(to_date))
  AND (sqlc.narg(account_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM voucher_lines l WHERE l.voucher_id = v.id AND l.account_id = sqlc.narg(account_id)));

-- name: GetVoucher :one
SELECT v.*, cu.name AS created_by_name, pu.name AS posted_by_name, r.doc_no AS reversal_of_no,
       rv.doc_no AS reversed_by_no
FROM vouchers v
LEFT JOIN users cu ON cu.id = v.created_by
LEFT JOIN users pu ON pu.id = v.posted_by
LEFT JOIN vouchers r ON r.id = v.reversal_of
LEFT JOIN vouchers rv ON rv.reversal_of = v.id
WHERE v.id = @id AND v.company_id = @company_id;

-- name: LockVoucher :one
SELECT * FROM vouchers WHERE id = @id AND company_id = @company_id FOR UPDATE;

-- name: CreateVoucher :one
INSERT INTO vouchers (company_id, doc_no, voucher_date, source_type, source_id, source_no, description, status,
                      reversal_of, total_amount, posted_by, posted_at, created_by, updated_by)
VALUES (@company_id, @doc_no, @voucher_date, @source_type, sqlc.narg(source_id), @source_no, @description, @status::text,
        sqlc.narg(reversal_of), @total_amount, sqlc.narg(posted_by),
        CASE WHEN @status::text = 'posted' THEN now() END, @created_by, @created_by)
RETURNING *;

-- name: UpdateVoucherHeader :one
UPDATE vouchers
SET voucher_date = @voucher_date, description = @description, total_amount = @total_amount,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: SetVoucherStatus :one
UPDATE vouchers
SET status = @status::text,
    posted_by = CASE WHEN @status::text = 'posted' THEN @actor_id::bigint ELSE posted_by END,
    posted_at = CASE WHEN @status::text = 'posted' THEN now() ELSE posted_at END,
    version = version + 1, updated_by = @actor_id::bigint
WHERE id = @id AND company_id = @company_id AND version = @version AND status = 'draft'
RETURNING *;

-- name: ListVoucherLines :many
SELECT l.*, a.code AS account_code, a.name AS account_name, c.name AS customer_name, s.name AS supplier_name,
       d.name AS department_name
FROM voucher_lines l
JOIN accounts a ON a.id = l.account_id
LEFT JOIN customers c ON c.id = l.customer_id
LEFT JOIN suppliers s ON s.id = l.supplier_id
LEFT JOIN departments d ON d.id = l.department_id
WHERE l.voucher_id = @voucher_id
ORDER BY l.line_no;

-- name: DeleteVoucherLines :exec
DELETE FROM voucher_lines WHERE voucher_id = @voucher_id;

-- name: AddVoucherLine :exec
INSERT INTO voucher_lines (voucher_id, line_no, account_id, debit, credit, description, customer_id, supplier_id, department_id)
VALUES (@voucher_id, @line_no, @account_id, @debit, @credit, @description, sqlc.narg(customer_id),
        sqlc.narg(supplier_id), sqlc.narg(department_id));

-- name: OpenVouchersBySource :many
-- 某張業務單據尚未被沖銷的已過帳傳票(反過帳時沖銷)
SELECT v.* FROM vouchers v
WHERE v.company_id = @company_id AND v.source_type = @source_type AND v.source_id = @source_id
  AND v.status = 'posted' AND v.reversal_of IS NULL
  AND NOT EXISTS (SELECT 1 FROM vouchers r WHERE r.reversal_of = v.id)
ORDER BY v.id;

-- name: CustomerSupplierDeptExist :one
SELECT
    COALESCE(sqlc.narg(customer_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM customers WHERE id = sqlc.narg(customer_id)::bigint AND company_id = @company_id::bigint), FALSE)::boolean AS customer_ok,
    COALESCE(sqlc.narg(supplier_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM suppliers WHERE id = sqlc.narg(supplier_id)::bigint AND company_id = @company_id::bigint), FALSE)::boolean AS supplier_ok,
    COALESCE(sqlc.narg(department_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM departments WHERE id = sqlc.narg(department_id)::bigint AND company_id = @company_id::bigint), FALSE)::boolean AS department_ok;

-- ======== 報表(只計已過帳傳票) ========

-- name: TrialBalance :many
SELECT a.id AS account_id, a.code, a.name, a.acct_type,
       COALESCE(SUM(l.debit - l.credit) FILTER (WHERE v.voucher_date < sqlc.arg(from_date)::date), 0)::numeric AS opening,
       COALESCE(SUM(l.debit) FILTER (WHERE v.voucher_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date), 0)::numeric AS period_debit,
       COALESCE(SUM(l.credit) FILTER (WHERE v.voucher_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date), 0)::numeric AS period_credit
FROM accounts a
JOIN voucher_lines l ON l.account_id = a.id
JOIN vouchers v ON v.id = l.voucher_id AND v.status = 'posted' AND v.voucher_date <= sqlc.arg(to_date)::date
WHERE a.company_id = @company_id
GROUP BY a.id
ORDER BY a.code;

-- name: GeneralLedgerOpening :one
SELECT COALESCE(SUM(l.debit - l.credit), 0)::numeric AS opening
FROM voucher_lines l JOIN vouchers v ON v.id = l.voucher_id
WHERE v.company_id = @company_id AND v.status = 'posted' AND l.account_id = @account_id
  AND v.voucher_date < sqlc.arg(from_date)::date;

-- name: GeneralLedger :many
SELECT v.id AS voucher_id, v.doc_no, v.voucher_date, v.source_type, v.source_no,
       COALESCE(NULLIF(l.description, ''), v.description) AS description, l.debit, l.credit
FROM voucher_lines l JOIN vouchers v ON v.id = l.voucher_id
WHERE v.company_id = @company_id AND v.status = 'posted' AND l.account_id = @account_id
  AND v.voucher_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY v.voucher_date, v.id, l.line_no;

-- name: Journal :many
SELECT v.id AS voucher_id, v.doc_no, v.voucher_date, v.source_type, v.source_no, v.description AS voucher_description,
       l.line_no, a.code AS account_code, a.name AS account_name, l.debit, l.credit, l.description
FROM vouchers v
JOIN voucher_lines l ON l.voucher_id = v.id
JOIN accounts a ON a.id = l.account_id
WHERE v.company_id = @company_id AND v.status = 'posted'
  AND v.voucher_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY v.voucher_date, v.id, l.line_no
LIMIT @lim OFFSET @off;

-- name: CountJournal :one
SELECT count(*) FROM vouchers v JOIN voucher_lines l ON l.voucher_id = v.id
WHERE v.company_id = @company_id AND v.status = 'posted'
  AND v.voucher_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date;
