-- 通知信(D88):交易內寫入寄信佇列,背景程式再寄出(寄信失敗不影響單據交易)
CREATE TABLE notifications (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id      BIGINT       NOT NULL REFERENCES companies (id),
    user_id         BIGINT       NOT NULL REFERENCES users (id),
    to_email        VARCHAR(255) NOT NULL,
    event           VARCHAR(30)  NOT NULL,                       -- approval_pending / check_due / test
    subject         VARCHAR(200) NOT NULL,
    body            TEXT         NOT NULL,
    dedupe_key      VARCHAR(100) NOT NULL DEFAULT '',            -- 同一個人同一個 key 只通知一次
    status          VARCHAR(10)  NOT NULL DEFAULT 'pending',     -- pending / sent / failed
    attempts        INTEGER      NOT NULL DEFAULT 0,
    last_error      TEXT         NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ,
    CONSTRAINT notifications_status_check CHECK (status IN ('pending', 'sent', 'failed'))
);
CREATE INDEX notifications_queue_idx ON notifications (status, next_attempt_at);
CREATE UNIQUE INDEX notifications_dedupe_idx ON notifications (user_id, dedupe_key) WHERE dedupe_key <> '';
