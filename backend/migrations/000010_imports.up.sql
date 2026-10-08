-- M7 資料匯入:批次紀錄、期初餘額用的序號

CREATE TABLE import_batches (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id BIGINT       NOT NULL REFERENCES companies (id),
    import_type VARCHAR(30) NOT NULL,
    filename   VARCHAR(255) NOT NULL DEFAULT '',
    row_count  INTEGER      NOT NULL DEFAULT 0,
    summary    VARCHAR(255) NOT NULL DEFAULT '',
    created_by BIGINT       REFERENCES users (id),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    undone_by  BIGINT       REFERENCES users (id),   -- 已撤銷(僅期初庫存、期初應收 / 應付、期初科目餘額可撤銷)
    undone_at  TIMESTAMPTZ
);
CREATE INDEX import_batches_company_idx ON import_batches (company_id, id DESC);

-- 期初應收 / 應付沒有來源單據,以序號當 source_id(accounts_receivable / payable 的 (source_type, source_id) 須唯一)
CREATE SEQUENCE opening_seq;

-- 期初應收 / 應付記錄來源匯入批次,撤銷時據此找回
ALTER TABLE accounts_receivable ADD COLUMN import_batch_id BIGINT REFERENCES import_batches (id);
ALTER TABLE accounts_payable ADD COLUMN import_batch_id BIGINT REFERENCES import_batches (id);
