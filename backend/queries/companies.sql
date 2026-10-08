-- name: GetCompany :one
SELECT * FROM companies WHERE id = @id;

-- name: GetCompanyByCode :one
SELECT * FROM companies WHERE code = @code;

-- name: UpdateCompany :one
UPDATE companies SET name = @name, tax_id = sqlc.narg(tax_id), tax_reg_no = @tax_reg_no, version = version + 1
WHERE id = @id AND version = @version RETURNING *;
