-- 專案:傳票明細的輔助核算維度,供專案別損益使用
CREATE TABLE projects (
    id         BIGSERIAL    PRIMARY KEY,
    company_id BIGINT       NOT NULL REFERENCES companies (id),
    code       VARCHAR(20)  NOT NULL,
    name       VARCHAR(100) NOT NULL,
    is_active  BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by BIGINT       REFERENCES users (id),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (company_id, code)
);
ALTER TABLE voucher_lines ADD COLUMN project_id BIGINT REFERENCES projects (id);
CREATE INDEX voucher_lines_project_idx ON voucher_lines (project_id) WHERE project_id IS NOT NULL;
