-- name: ListRoles :many
SELECT r.*, (SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id) AS user_count
FROM roles r
WHERE r.company_id = @company_id
ORDER BY r.code;

-- name: GetRole :one
SELECT * FROM roles WHERE id = @id AND company_id = @company_id;

-- name: CountRolesByIDs :one
SELECT count(*) FROM roles WHERE company_id = @company_id AND id = ANY(@ids::bigint[]);

-- name: CreateRole :one
INSERT INTO roles (company_id, code, name, description, data_scope, created_by, updated_by)
VALUES (@company_id, @code, @name, @description, @data_scope, @created_by, @created_by)
RETURNING *;

-- name: UpdateRole :one
UPDATE roles
SET code        = @code,
    name        = @name,
    description = @description,
    data_scope  = @data_scope,
    is_active   = @is_active,
    version     = version + 1,
    updated_by  = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: DeleteRole :execrows
DELETE FROM roles WHERE id = @id AND company_id = @company_id;

-- name: ListRolePermissions :many
SELECT permission FROM role_permissions WHERE role_id = @role_id ORDER BY permission;

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions WHERE role_id = @role_id;

-- name: AddRolePermissions :exec
INSERT INTO role_permissions (role_id, permission)
SELECT @role_id, unnest(@permissions::text[]);

-- name: CountRolesUsers :one
SELECT count(*) FROM user_roles WHERE role_id = @role_id;
