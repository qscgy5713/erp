-- name: FxOpenReceivables :many
-- 截止日(as_of)的外幣應收未沖餘額:原幣餘額與帳面本位幣金額,依幣別彙總(回推到截止日,同帳齡)
SELECT r.currency::text AS currency,
       COALESCE(SUM(r.amount - pd.paid), 0)::numeric AS foreign_balance,
       COALESCE(SUM(CASE WHEN r.amount = 0 THEN 0 ELSE r.base_amount * (r.amount - pd.paid) / r.amount END), 0)::numeric AS carrying
FROM accounts_receivable r
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(sl.amount), 0) AS paid
    FROM settlement_lines sl JOIN settlements st ON st.id = sl.settlement_id
    WHERE sl.receivable_id = r.id AND st.status = 'posted' AND st.doc_date <= @as_of::date
) pd
WHERE r.company_id = @company_id AND r.currency <> @base_currency AND r.amount <> pd.paid AND r.doc_date <= @as_of::date
GROUP BY r.currency ORDER BY r.currency;

-- name: FxOpenPayables :many
SELECT p.currency::text AS currency,
       COALESCE(SUM(p.amount - pd.paid), 0)::numeric AS foreign_balance,
       COALESCE(SUM(CASE WHEN p.amount = 0 THEN 0 ELSE p.base_amount * (p.amount - pd.paid) / p.amount END), 0)::numeric AS carrying
FROM accounts_payable p
CROSS JOIN LATERAL (
    SELECT COALESCE(SUM(sl.amount), 0) AS paid
    FROM settlement_lines sl JOIN settlements st ON st.id = sl.settlement_id
    WHERE sl.payable_id = p.id AND st.status = 'posted' AND st.doc_date <= @as_of::date
) pd
WHERE p.company_id = @company_id AND p.currency <> @base_currency AND p.amount <> pd.paid AND p.doc_date <= @as_of::date
GROUP BY p.currency ORDER BY p.currency;

-- name: ListFxRevaluations :many
SELECT * FROM fx_revaluations WHERE company_id = @company_id ORDER BY period DESC, id DESC LIMIT 24;

-- name: LatestActiveFxRevaluation :one
SELECT * FROM fx_revaluations WHERE company_id = @company_id AND status = 'active' ORDER BY period DESC LIMIT 1 FOR UPDATE;

-- name: CreateFxRevaluation :one
INSERT INTO fx_revaluations (company_id, period, ar_diff, ap_diff, created_by)
VALUES (@company_id, @period, @ar_diff, @ap_diff, @created_by) RETURNING *;

-- name: SetFxRevaluationVouchers :exec
UPDATE fx_revaluations SET voucher_id = sqlc.narg(voucher_id), prev_revaluation_id = sqlc.narg(prev_revaluation_id)
WHERE id = @id;

-- name: SetFxRevaluationStatus :exec
UPDATE fx_revaluations SET status = @status WHERE id = @id;

-- name: OpenVoucherBySourceID :one
-- 某業務單據尚未被沖銷的已過帳傳票(單張)
SELECT v.id FROM vouchers v
WHERE v.company_id = @company_id AND v.source_type = @source_type AND v.source_id = @source_id
  AND v.status = 'posted' AND v.reversal_of IS NULL
  AND NOT EXISTS (SELECT 1 FROM vouchers r WHERE r.reversal_of = v.id)
ORDER BY v.id LIMIT 1;
