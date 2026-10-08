-- ======== 雙因素驗證(D66) ========

-- name: SetTOTPPending :exec
-- 產生新的待驗證密鑰(覆蓋舊的暫存值;已啟用者不可呼叫,程式先檢查)
UPDATE users SET totp_secret_enc = @secret_enc, totp_enabled = FALSE, totp_enabled_at = NULL, totp_last_step = 0
WHERE id = @id;

-- name: EnableTOTP :exec
UPDATE users SET totp_enabled = TRUE, totp_enabled_at = now(), totp_last_step = @last_step WHERE id = @id;

-- name: DisableTOTP :exec
UPDATE users SET totp_secret_enc = NULL, totp_enabled = FALSE, totp_enabled_at = NULL, totp_last_step = 0 WHERE id = @id;

-- name: AdvanceTOTPStep :execrows
-- 只有時間步比上次大才接受(同一個驗證碼不可重複使用);回傳 0 表示重放
UPDATE users SET totp_last_step = @step WHERE id = @id AND totp_last_step < @step;

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes WHERE user_id = @user_id;

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: UseRecoveryCode :execrows
UPDATE user_recovery_codes SET used_at = now() WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes WHERE user_id = @user_id AND used_at IS NULL;

-- name: CompanyRequires2FA :one
SELECT require_2fa FROM companies WHERE id = @id;

-- name: SetCompanyRequire2FA :exec
UPDATE companies SET require_2fa = @require WHERE id = @id;

-- name: BumpTokenVersion :exec
UPDATE users SET token_version = token_version + 1 WHERE id = @id;
