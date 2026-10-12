-- name: CreateCheck :exec
INSERT INTO checks (company_id, side, settlement_id, customer_id, supplier_id, check_no, amount, due_date)
VALUES (@company_id, @side, @settlement_id, sqlc.narg(customer_id), sqlc.narg(supplier_id), @check_no, @amount, @due_date);

-- name: GetCheckBySettlement :one
SELECT * FROM checks WHERE settlement_id = @settlement_id;

-- name: DeleteCheckBySettlement :exec
DELETE FROM checks WHERE settlement_id = @settlement_id;

-- name: LockCheck :one
SELECT * FROM checks WHERE id = @id AND company_id = @company_id AND side = @side FOR UPDATE;

-- name: SetCheckCleared :exec
UPDATE checks SET status = @status, cleared_date = sqlc.narg(cleared_date), cleared_by = sqlc.narg(cleared_by)
WHERE id = @id;

-- name: ListChecks :many
SELECT k.id, k.side, k.settlement_id, s.doc_no AS settlement_no, k.check_no, k.amount, k.due_date, k.status,
       k.cleared_date, COALESCE(c.code, sp.code) AS partner_code, COALESCE(c.name, sp.name) AS partner_name
FROM checks k
JOIN settlements s ON s.id = k.settlement_id
LEFT JOIN customers c ON c.id = k.customer_id
LEFT JOIN suppliers sp ON sp.id = k.supplier_id
WHERE k.company_id = @company_id AND k.side = @side
  AND (sqlc.narg(status)::text IS NULL OR k.status = sqlc.narg(status))
  AND (sqlc.narg(due_to)::date IS NULL OR k.due_date <= sqlc.narg(due_to))
ORDER BY k.due_date, k.id
LIMIT 500;
