-- name: ListCustomers :many
-- scope_* 為資料範圍:依負責業務員(本人 / 同部門)過濾;未指定負責業務的客戶只有「全部」範圍看得到
SELECT c.*, s.name AS sales_user_name
FROM customers c
LEFT JOIN users s ON s.id = c.sales_user_id
WHERE c.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR c.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.short_name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.tax_id = sqlc.narg(keyword))
  AND (sqlc.narg(is_active)::boolean IS NULL OR c.is_active = sqlc.narg(is_active))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR s.department_id = sqlc.narg(scope_dept_id))
ORDER BY c.code
LIMIT @lim OFFSET @off;

-- name: CountCustomers :one
SELECT count(*)
FROM customers c
LEFT JOIN users s ON s.id = c.sales_user_id
WHERE c.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR c.code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.short_name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR c.tax_id = sqlc.narg(keyword))
  AND (sqlc.narg(is_active)::boolean IS NULL OR c.is_active = sqlc.narg(is_active))
  AND (sqlc.narg(scope_user_id)::bigint IS NULL OR c.sales_user_id = sqlc.narg(scope_user_id))
  AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR s.department_id = sqlc.narg(scope_dept_id));

-- name: GetCustomer :one
SELECT c.*, s.name AS sales_user_name, s.department_id AS sales_department_id
FROM customers c
LEFT JOIN users s ON s.id = c.sales_user_id
WHERE c.id = @id AND c.company_id = @company_id;

-- name: CheckPartnerRefs :one
SELECT
    EXISTS (SELECT 1 FROM currencies cu WHERE cu.code = @currency::text AND cu.is_active) AS currency_ok,
    COALESCE(sqlc.narg(tax_type_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM tax_types tt
        WHERE tt.id = sqlc.narg(tax_type_id)::bigint AND tt.company_id = @company_id::bigint), FALSE)::boolean AS tax_type_ok,
    COALESCE(sqlc.narg(payment_term_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM payment_terms pt
        WHERE pt.id = sqlc.narg(payment_term_id)::bigint AND pt.company_id = @company_id::bigint), FALSE)::boolean AS payment_term_ok,
    COALESCE(sqlc.narg(sales_user_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM users us
        WHERE us.id = sqlc.narg(sales_user_id)::bigint AND us.company_id = @company_id::bigint), FALSE)::boolean AS sales_user_ok;

-- name: GetUserDepartment :one
SELECT department_id FROM users WHERE id = @id AND company_id = @company_id;

-- name: CreateCustomer :one
INSERT INTO customers (company_id, code, name, short_name, tax_id, invoice_title, phone, email, contacts,
                       addresses, currency, tax_type_id, payment_term_id, credit_limit, sales_user_id, note,
                       created_by, updated_by)
VALUES (@company_id, @code, @name, @short_name, sqlc.narg(tax_id), @invoice_title, @phone, @email, @contacts,
        @addresses, @currency, sqlc.narg(tax_type_id), sqlc.narg(payment_term_id), @credit_limit,
        sqlc.narg(sales_user_id), @note, @created_by, @created_by)
RETURNING *;

-- name: UpdateCustomer :one
UPDATE customers
SET code = @code, name = @name, short_name = @short_name, tax_id = sqlc.narg(tax_id),
    invoice_title = @invoice_title, phone = @phone, email = @email, contacts = @contacts,
    addresses = @addresses, currency = @currency, tax_type_id = sqlc.narg(tax_type_id),
    payment_term_id = sqlc.narg(payment_term_id), credit_limit = @credit_limit,
    sales_user_id = sqlc.narg(sales_user_id), note = @note, is_active = @is_active,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: ListSuppliers :many
SELECT * FROM suppliers
WHERE company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR short_name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR tax_id = sqlc.narg(keyword))
  AND (sqlc.narg(is_active)::boolean IS NULL OR is_active = sqlc.narg(is_active))
ORDER BY code
LIMIT @lim OFFSET @off;

-- name: CountSuppliers :one
SELECT count(*) FROM suppliers
WHERE company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR code ILIKE '%' || sqlc.narg(keyword) || '%'
       OR name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR short_name ILIKE '%' || sqlc.narg(keyword) || '%'
       OR tax_id = sqlc.narg(keyword))
  AND (sqlc.narg(is_active)::boolean IS NULL OR is_active = sqlc.narg(is_active));

-- name: GetSupplier :one
SELECT * FROM suppliers WHERE id = @id AND company_id = @company_id;

-- name: CreateSupplier :one
INSERT INTO suppliers (company_id, code, name, short_name, tax_id, phone, email, contacts, addresses, currency,
                       tax_type_id, payment_term_id, bank_name, bank_account, note, created_by, updated_by)
VALUES (@company_id, @code, @name, @short_name, sqlc.narg(tax_id), @phone, @email, @contacts, @addresses, @currency,
        sqlc.narg(tax_type_id), sqlc.narg(payment_term_id), @bank_name, @bank_account, @note,
        @created_by, @created_by)
RETURNING *;

-- name: UpdateSupplier :one
UPDATE suppliers
SET code = @code, name = @name, short_name = @short_name, tax_id = sqlc.narg(tax_id), phone = @phone,
    email = @email, contacts = @contacts, addresses = @addresses, currency = @currency,
    tax_type_id = sqlc.narg(tax_type_id), payment_term_id = sqlc.narg(payment_term_id),
    bank_name = @bank_name, bank_account = @bank_account, note = @note, is_active = @is_active,
    version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;
