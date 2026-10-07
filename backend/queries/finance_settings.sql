-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY sort_order, code;

-- name: GetCurrency :one
SELECT * FROM currencies WHERE code = @code;

-- name: SetCurrencyActive :one
UPDATE currencies SET is_active = @is_active WHERE code = @code RETURNING *;

-- name: ListExchangeRates :many
SELECT * FROM exchange_rates
WHERE company_id = @company_id
  AND (sqlc.narg(currency)::text IS NULL OR currency = sqlc.narg(currency))
  AND (sqlc.narg(from_date)::date IS NULL OR rate_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR rate_date <= sqlc.narg(to_date))
ORDER BY rate_date DESC, currency
LIMIT @lim OFFSET @off;

-- name: CountExchangeRates :one
SELECT count(*) FROM exchange_rates
WHERE company_id = @company_id
  AND (sqlc.narg(currency)::text IS NULL OR currency = sqlc.narg(currency))
  AND (sqlc.narg(from_date)::date IS NULL OR rate_date >= sqlc.narg(from_date))
  AND (sqlc.narg(to_date)::date IS NULL OR rate_date <= sqlc.narg(to_date));

-- name: GetExchangeRate :one
SELECT * FROM exchange_rates WHERE id = @id AND company_id = @company_id;

-- name: CreateExchangeRate :one
INSERT INTO exchange_rates (company_id, currency, rate_date, rate, created_by, updated_by)
VALUES (@company_id, @currency, @rate_date, @rate, @created_by, @created_by)
RETURNING *;

-- name: UpdateExchangeRate :one
UPDATE exchange_rates
SET rate = @rate, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: DeleteExchangeRate :execrows
DELETE FROM exchange_rates WHERE id = @id AND company_id = @company_id;

-- name: RateOn :one
-- 取該日或之前最近一筆匯率
SELECT * FROM exchange_rates
WHERE company_id = @company_id AND currency = @currency AND rate_date <= @on_date
ORDER BY rate_date DESC
LIMIT 1;

-- name: ListTaxTypes :many
SELECT * FROM tax_types WHERE company_id = @company_id ORDER BY code;

-- name: GetTaxType :one
SELECT * FROM tax_types WHERE id = @id AND company_id = @company_id;

-- name: CreateTaxType :one
INSERT INTO tax_types (company_id, code, name, kind, rate, created_by, updated_by)
VALUES (@company_id, @code, @name, @kind, @rate, @created_by, @created_by)
RETURNING *;

-- name: UpdateTaxType :one
UPDATE tax_types
SET code = @code, name = @name, kind = @kind, rate = @rate, is_active = @is_active,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListPaymentTerms :many
SELECT * FROM payment_terms WHERE company_id = @company_id ORDER BY code;

-- name: GetPaymentTerm :one
SELECT * FROM payment_terms WHERE id = @id AND company_id = @company_id;

-- name: CreatePaymentTerm :one
INSERT INTO payment_terms (company_id, code, name, is_month_end, net_days, created_by, updated_by)
VALUES (@company_id, @code, @name, @is_month_end, @net_days, @created_by, @created_by)
RETURNING *;

-- name: UpdatePaymentTerm :one
UPDATE payment_terms
SET code = @code, name = @name, is_month_end = @is_month_end, net_days = @net_days,
    is_active = @is_active, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;
