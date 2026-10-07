-- name: InsertAuditLog :exec
INSERT INTO audit_logs (company_id, user_id, action, entity_type, entity_id, summary,
                        before_data, after_data, ip, user_agent, request_id)
VALUES (@company_id, sqlc.narg(user_id), @action, @entity_type, sqlc.narg(entity_id), @summary,
        sqlc.narg(before_data), sqlc.narg(after_data), @ip, @user_agent, @request_id);

-- name: ListAuditLogs :many
SELECT a.id, a.user_id, u.username, u.name AS user_name, a.action, a.entity_type, a.entity_id,
       a.summary, a.before_data, a.after_data, a.ip, a.user_agent, a.request_id, a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
WHERE a.company_id = @company_id
  AND (sqlc.narg(entity_type)::text IS NULL OR a.entity_type = sqlc.narg(entity_type))
  AND (sqlc.narg(entity_id)::bigint IS NULL OR a.entity_id = sqlc.narg(entity_id))
  AND (sqlc.narg(user_id)::bigint IS NULL OR a.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_time))
ORDER BY a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountAuditLogs :one
SELECT count(*)
FROM audit_logs a
WHERE a.company_id = @company_id
  AND (sqlc.narg(entity_type)::text IS NULL OR a.entity_type = sqlc.narg(entity_type))
  AND (sqlc.narg(entity_id)::bigint IS NULL OR a.entity_id = sqlc.narg(entity_id))
  AND (sqlc.narg(user_id)::bigint IS NULL OR a.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_time));
