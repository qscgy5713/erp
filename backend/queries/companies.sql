-- name: GetCompany :one
SELECT * FROM companies WHERE id = @id;

-- name: GetCompanyByCode :one
SELECT * FROM companies WHERE code = @code;
