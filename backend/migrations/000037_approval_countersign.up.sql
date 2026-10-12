-- 簽核會簽 / 或簽(D95):一層可指定多個角色
-- 規則:同一 step_no 的多列即為該層的多個角色,mode 一致(any 或簽 / all 會簽)
ALTER TABLE approval_rule_steps ADD COLUMN mode VARCHAR(3) NOT NULL DEFAULT 'any';
ALTER TABLE approval_rule_steps ADD CONSTRAINT approval_rule_steps_mode_check CHECK (mode IN ('any', 'all'));
ALTER TABLE approval_rule_steps DROP CONSTRAINT approval_rule_steps_pkey;
ALTER TABLE approval_rule_steps ADD PRIMARY KEY (rule_id, step_no, role_id);

-- 單據的簽核格:一層可有多列(slot_no 從 1 起算,第 1 層固定一格)
ALTER TABLE document_approvals ADD COLUMN slot_no INTEGER NOT NULL DEFAULT 1;
ALTER TABLE document_approvals ADD COLUMN mode VARCHAR(3) NOT NULL DEFAULT 'any';
ALTER TABLE document_approvals ADD CONSTRAINT document_approvals_mode_check CHECK (mode IN ('any', 'all'));
ALTER TABLE document_approvals DROP CONSTRAINT document_approvals_key;
ALTER TABLE document_approvals ADD CONSTRAINT document_approvals_key UNIQUE (doc_type, doc_id, step_no, slot_no);
