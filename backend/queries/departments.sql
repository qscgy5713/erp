-- name: ListDepartments :many
SELECT d.*, (SELECT count(*) FROM users u WHERE u.department_id = d.id) AS user_count
FROM departments d
WHERE d.company_id = @company_id
ORDER BY d.sort_order, d.code;

-- name: GetDepartment :one
SELECT * FROM departments WHERE id = @id AND company_id = @company_id;

-- name: CreateDepartment :one
INSERT INTO departments (company_id, parent_id, code, name, sort_order, created_by, updated_by)
VALUES (@company_id, sqlc.narg(parent_id), @code, @name, @sort_order, @created_by, @created_by)
RETURNING *;

-- name: UpdateDepartment :one
UPDATE departments
SET parent_id  = sqlc.narg(parent_id),
    code       = @code,
    name       = @name,
    sort_order = @sort_order,
    is_active  = @is_active,
    version    = version + 1,
    updated_by = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: IsDepartmentDescendant :one
-- candidate 是否為 ancestor 本身或其子孫(用來防止設定循環的上層部門)
WITH RECURSIVE tree (dept_id) AS (
    SELECT departments.id FROM departments WHERE departments.id = @ancestor_id::bigint
    UNION ALL
    SELECT departments.id FROM departments, tree WHERE departments.parent_id = tree.dept_id
)
SELECT EXISTS (SELECT 1 FROM tree WHERE dept_id = @candidate_id::bigint);
