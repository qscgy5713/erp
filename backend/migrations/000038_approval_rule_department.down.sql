DELETE FROM approval_rules WHERE department_id IS NOT NULL;
ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_key;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_key UNIQUE (company_id, doc_type, min_amount);
ALTER TABLE approval_rules DROP COLUMN department_id;
