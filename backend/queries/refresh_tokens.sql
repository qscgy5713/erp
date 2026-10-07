-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at, ip, user_agent)
VALUES (@user_id, @family_id, @token_hash, @expires_at, @ip, @user_agent)
RETURNING *;

-- name: GetRefreshTokenForUpdate :one
SELECT * FROM refresh_tokens WHERE token_hash = @token_hash FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeRefreshFamily :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: DeleteStaleRefreshTokens :execrows
-- 清理過期或撤銷超過保留期的紀錄
DELETE FROM refresh_tokens
WHERE expires_at < now() - make_interval(days => @keep_days::int)
   OR revoked_at < now() - make_interval(days => @keep_days::int);
