-- name: ListProjects :many
SELECT id, code, name, is_active FROM projects WHERE company_id = @company_id ORDER BY code;

-- name: CreateProject :one
INSERT INTO projects (company_id, code, name, created_by) VALUES (@company_id, @code, @name, @created_by)
RETURNING id, code, name, is_active;

-- name: UpdateProject :one
UPDATE projects SET name = @name, is_active = @is_active
WHERE id = @id AND company_id = @company_id
RETURNING id, code, name, is_active;

-- name: ProjectPnl :many
-- 專案別損益:期間內已過帳傳票的收入 / 成本 / 費用,依明細的專案分組(沒填專案的歸在「未指定」)
SELECT l.project_id, p.code AS project_code, p.name AS project_name, a.acct_type,
       COALESCE(SUM(l.credit - l.debit), 0)::numeric AS amount
FROM voucher_lines l
JOIN vouchers v ON v.id = l.voucher_id AND v.status = 'posted' AND v.source_type <> 'year_end'
JOIN accounts a ON a.id = l.account_id
LEFT JOIN projects p ON p.id = l.project_id
WHERE v.company_id = @company_id AND v.voucher_date BETWEEN @from_date::date AND @to_date::date
  AND a.acct_type IN ('revenue', 'cost', 'expense')
GROUP BY l.project_id, p.code, p.name, a.acct_type
ORDER BY p.code NULLS LAST, a.acct_type;
