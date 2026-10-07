-- name: GetUserByUsername :one
SELECT * FROM users WHERE lower(username) = lower(@username);

-- name: GetUser :one
SELECT * FROM users WHERE id = @id AND company_id = @company_id;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: ListUsers :many
SELECT u.*, d.name AS department_name
FROM users u
LEFT JOIN departments d ON d.id = u.department_id
WHERE u.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR u.username ILIKE '%' || sqlc.narg(keyword) || '%'
       OR u.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(department_id)::bigint IS NULL OR u.department_id = sqlc.narg(department_id))
  AND (sqlc.narg(is_active)::boolean IS NULL OR u.is_active = sqlc.narg(is_active))
ORDER BY u.id
LIMIT @lim OFFSET @off;

-- name: CountUsers :one
SELECT count(*)
FROM users u
WHERE u.company_id = @company_id
  AND (sqlc.narg(keyword)::text IS NULL
       OR u.username ILIKE '%' || sqlc.narg(keyword) || '%'
       OR u.name ILIKE '%' || sqlc.narg(keyword) || '%')
  AND (sqlc.narg(department_id)::bigint IS NULL OR u.department_id = sqlc.narg(department_id))
  AND (sqlc.narg(is_active)::boolean IS NULL OR u.is_active = sqlc.narg(is_active));

-- name: CreateUser :one
INSERT INTO users (company_id, department_id, username, name, email, password_hash,
                   is_superadmin, must_change_password, created_by, updated_by)
VALUES (@company_id, sqlc.narg(department_id), @username, @name, sqlc.narg(email), @password_hash,
        @is_superadmin, @must_change_password, sqlc.narg(created_by), sqlc.narg(created_by))
RETURNING *;

-- name: UpdateUser :one
-- 樂觀鎖:version 不符時不會更新任何列
UPDATE users
SET department_id = sqlc.narg(department_id),
    name          = @name,
    email         = sqlc.narg(email),
    is_active     = @is_active,
    -- 停用時讓既有 token 立即失效
    token_version = CASE WHEN is_active AND NOT @is_active THEN token_version + 1 ELSE token_version END,
    version       = version + 1,
    updated_by    = @updated_by
WHERE id = @id AND company_id = @company_id AND version = @version
RETURNING *;

-- name: RecordLoginFailure :one
-- 失敗次數達上限即鎖定,並將計數歸零
UPDATE users
SET failed_login_count = CASE WHEN failed_login_count + 1 >= @max_attempts::int THEN 0
                              ELSE failed_login_count + 1 END,
    locked_until       = CASE WHEN failed_login_count + 1 >= @max_attempts::int
                              THEN now() + make_interval(secs => @lock_seconds::int)
                              ELSE locked_until END
WHERE id = @id
RETURNING failed_login_count, locked_until;

-- name: RecordLoginSuccess :exec
UPDATE users
SET failed_login_count = 0, locked_until = NULL, last_login_at = now()
WHERE id = @id;

-- name: SetPassword :one
UPDATE users
SET password_hash        = @password_hash,
    password_changed_at  = now(),
    must_change_password = @must_change_password,
    token_version        = token_version + 1,
    failed_login_count   = 0,
    locked_until         = NULL,
    version              = version + 1,
    updated_by           = @updated_by
WHERE id = @id
RETURNING *;

-- name: UnlockUser :exec
UPDATE users
SET failed_login_count = 0, locked_until = NULL, version = version + 1, updated_by = @updated_by
WHERE id = @id AND company_id = @company_id;

-- name: ListUserRoleIDs :many
SELECT role_id FROM user_roles WHERE user_id = @user_id ORDER BY role_id;

-- name: DeleteUserRoles :exec
DELETE FROM user_roles WHERE user_id = @user_id;

-- name: AddUserRoles :exec
INSERT INTO user_roles (user_id, role_id)
SELECT @user_id, unnest(@role_ids::bigint[]);

-- name: ListUserAccess :many
-- 登入者的有效角色(啟用中)與其權限;沒有權限的角色 permission 為 NULL
SELECT r.data_scope, rp.permission
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id AND r.is_active
LEFT JOIN role_permissions rp ON rp.role_id = r.id
WHERE ur.user_id = @user_id;

-- name: ListRolesForUsers :many
SELECT ur.user_id, r.id, r.name
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = ANY(@user_ids::bigint[])
ORDER BY r.id;
