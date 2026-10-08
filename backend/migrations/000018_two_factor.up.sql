-- 雙因素驗證(TOTP,D66)
ALTER TABLE users ADD COLUMN totp_secret_enc BYTEA;                    -- AES-GCM 加密後的密鑰;啟用前為待驗證的暫存值
ALTER TABLE users ADD COLUMN totp_enabled    BOOLEAN     NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN totp_enabled_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN totp_last_step  BIGINT      NOT NULL DEFAULT 0;   -- 最後一次使用的時間步,擋重放
ALTER TABLE users ADD CONSTRAINT users_totp_secret_check CHECK (NOT totp_enabled OR totp_secret_enc IS NOT NULL);

-- 備援碼:一次性,只存雜湊
CREATE TABLE user_recovery_codes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  BYTEA       NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_recovery_codes_key UNIQUE (user_id, code_hash)
);

-- 公司政策:要求所有使用者啟用雙因素驗證(未啟用者登入後只能先設定)
ALTER TABLE companies ADD COLUMN require_2fa BOOLEAN NOT NULL DEFAULT FALSE;
