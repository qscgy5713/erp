-- 簽核代理人(D87)與庫存單據納入多層簽核
CREATE TABLE approval_delegations (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id   BIGINT       NOT NULL REFERENCES companies (id),
    delegator_id BIGINT       NOT NULL REFERENCES users (id),   -- 委託人(出差 / 休假的人)
    delegate_id  BIGINT       NOT NULL REFERENCES users (id),   -- 代理人
    valid_from   DATE         NOT NULL,
    valid_to     DATE         NOT NULL,
    note         VARCHAR(200) NOT NULL DEFAULT '',
    created_by   BIGINT       REFERENCES users (id),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT approval_delegations_range_check CHECK (valid_to >= valid_from),
    CONSTRAINT approval_delegations_self_check CHECK (delegator_id <> delegate_id)
);
CREATE INDEX approval_delegations_delegate_idx ON approval_delegations (delegate_id, valid_from, valid_to);
CREATE INDEX approval_delegations_delegator_idx ON approval_delegations (delegator_id);

-- 代理核准時記錄「代誰核准」
ALTER TABLE document_approvals ADD COLUMN acting_for BIGINT REFERENCES users (id);

ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_doc_type_check;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
    ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment', 'work_order', 'stock_document'));
