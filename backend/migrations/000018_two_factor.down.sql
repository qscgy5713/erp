ALTER TABLE companies DROP COLUMN require_2fa;
DROP TABLE IF EXISTS user_recovery_codes;
ALTER TABLE users DROP CONSTRAINT users_totp_secret_check;
ALTER TABLE users DROP COLUMN totp_last_step, DROP COLUMN totp_enabled_at, DROP COLUMN totp_enabled, DROP COLUMN totp_secret_enc;
