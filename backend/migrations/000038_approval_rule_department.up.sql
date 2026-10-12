-- 簽核規則可限定部門(D96):依送審者所屬部門(含上層部門)套用;department_id 為 NULL = 全公司
ALTER TABLE approval_rules ADD COLUMN department_id BIGINT REFERENCES departments (id);
ALTER TABLE approval_rules DROP CONSTRAINT approval_rules_key;
ALTER TABLE approval_rules ADD CONSTRAINT approval_rules_key
    UNIQUE NULLS NOT DISTINCT (company_id, doc_type, department_id, min_amount);
