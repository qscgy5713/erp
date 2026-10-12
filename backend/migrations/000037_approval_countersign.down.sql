-- 還原為一層一個角色:每層只保留一個角色 / 一格
DELETE FROM approval_rule_steps s USING approval_rule_steps k
 WHERE s.rule_id = k.rule_id AND s.step_no = k.step_no AND s.role_id > k.role_id;
ALTER TABLE approval_rule_steps DROP CONSTRAINT approval_rule_steps_pkey;
ALTER TABLE approval_rule_steps ADD PRIMARY KEY (rule_id, step_no);
ALTER TABLE approval_rule_steps DROP CONSTRAINT approval_rule_steps_mode_check;
ALTER TABLE approval_rule_steps DROP COLUMN mode;

DELETE FROM document_approvals WHERE slot_no > 1;
ALTER TABLE document_approvals DROP CONSTRAINT document_approvals_key;
ALTER TABLE document_approvals ADD CONSTRAINT document_approvals_key UNIQUE (doc_type, doc_id, step_no);
ALTER TABLE document_approvals DROP CONSTRAINT document_approvals_mode_check;
ALTER TABLE document_approvals DROP COLUMN mode;
ALTER TABLE document_approvals DROP COLUMN slot_no;
