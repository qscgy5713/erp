-- 傳票附件:原始憑證的掃描檔或明細(PDF / 圖片 / Excel),內容存在資料庫,隨備份一起還原
CREATE TABLE voucher_attachments (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id   BIGINT       NOT NULL REFERENCES companies (id),
    voucher_id   BIGINT       NOT NULL REFERENCES vouchers (id),
    filename     VARCHAR(200) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    size_bytes   INTEGER      NOT NULL,
    content      BYTEA        NOT NULL,
    uploaded_by  BIGINT       REFERENCES users (id),
    uploaded_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT voucher_attachments_size_check CHECK (size_bytes > 0 AND size_bytes <= 5242880)
);
CREATE INDEX voucher_attachments_voucher_idx ON voucher_attachments (voucher_id);
