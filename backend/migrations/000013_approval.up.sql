-- M10 多層簽核:依單據類型與金額決定需要幾層核准(D61)
-- 第 1 層永遠是「具該單據核准權限的人」(與原本相同);規則只負責追加第 2 層以後要指定的角色。
CREATE TABLE approval_rules (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT        NOT NULL REFERENCES companies (id),
    doc_type    VARCHAR(20)   NOT NULL,
    -- 單據本位幣含稅金額 >= min_amount 時適用;同類型取 min_amount 最大且不超過單據金額的一條
    min_amount  NUMERIC(18,2) NOT NULL DEFAULT 0,
    created_by  BIGINT        REFERENCES users (id),
    updated_by  BIGINT        REFERENCES users (id),
    version     INTEGER       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT approval_rules_key UNIQUE (company_id, doc_type, min_amount),
    CONSTRAINT approval_rules_doc_type_check CHECK (doc_type IN
        ('purchase_order', 'goods_receipt', 'sales_order', 'delivery', 'collection', 'payment')),
    CONSTRAINT approval_rules_min_check CHECK (min_amount >= 0)
);
CREATE TRIGGER approval_rules_set_updated_at BEFORE UPDATE ON approval_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 第 2 層以後的角色(step_no 從 2 開始)
CREATE TABLE approval_rule_steps (
    rule_id  BIGINT  NOT NULL REFERENCES approval_rules (id) ON DELETE CASCADE,
    step_no  INTEGER NOT NULL,
    role_id  BIGINT  NOT NULL REFERENCES roles (id) ON DELETE RESTRICT,
    PRIMARY KEY (rule_id, step_no),
    CONSTRAINT approval_rule_steps_no_check CHECK (step_no BETWEEN 2 AND 5)
);

-- 送審時依規則快照出的流程:之後修改規則不影響進行中的單據;沒有任何列 = 單層核准(舊行為)
CREATE TABLE document_approvals (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id  BIGINT       NOT NULL REFERENCES companies (id),
    doc_type    VARCHAR(20)  NOT NULL,
    doc_id      BIGINT       NOT NULL,
    step_no     INTEGER      NOT NULL,
    role_id     BIGINT       REFERENCES roles (id) ON DELETE SET NULL,   -- 第 1 層為 NULL
    role_name   VARCHAR(100) NOT NULL DEFAULT '',                        -- 角色名稱快照
    approver_id BIGINT       REFERENCES users (id),
    approved_at TIMESTAMPTZ,
    CONSTRAINT document_approvals_key UNIQUE (doc_type, doc_id, step_no),
    CONSTRAINT document_approvals_done_check CHECK ((approver_id IS NULL) = (approved_at IS NULL))
);
CREATE INDEX document_approvals_company_idx ON document_approvals (company_id, doc_type);
