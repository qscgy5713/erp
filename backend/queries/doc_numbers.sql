-- name: ListDocNumberRules :many
SELECT * FROM doc_number_rules WHERE company_id = @company_id ORDER BY doc_type;

-- name: GetDocNumberRule :one
SELECT * FROM doc_number_rules WHERE company_id = @company_id AND doc_type = @doc_type;

-- name: UpdateDocNumberRule :one
UPDATE doc_number_rules
SET name        = @name,
    prefix      = @prefix,
    date_format = @date_format,
    seq_length  = @seq_length,
    version     = version + 1,
    updated_by  = @updated_by
WHERE company_id = @company_id AND doc_type = @doc_type AND version = @version
RETURNING *;

-- name: NextDocNumber :one
-- 原子地取下一號:同一期間的計數列會被鎖到交易結束,確保不重號、交易回滾時不跳號
INSERT INTO doc_number_counters (company_id, doc_type, period_key, last_value)
VALUES (@company_id, @doc_type, @period_key, 1)
ON CONFLICT (company_id, doc_type, period_key)
DO UPDATE SET last_value = doc_number_counters.last_value + 1
RETURNING last_value;
