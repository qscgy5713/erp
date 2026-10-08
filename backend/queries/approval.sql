-- ======== 多層簽核(D61) ========

-- name: ListApprovalRules :many
SELECT * FROM approval_rules WHERE company_id = @company_id ORDER BY doc_type, min_amount;

-- name: ListApprovalRuleSteps :many
SELECT s.rule_id, s.step_no, s.role_id, r.name AS role_name
FROM approval_rule_steps s JOIN roles r ON r.id = s.role_id
JOIN approval_rules ar ON ar.id = s.rule_id
WHERE ar.company_id = @company_id ORDER BY s.rule_id, s.step_no;

-- name: GetApprovalRule :one
SELECT * FROM approval_rules WHERE id = @id AND company_id = @company_id;

-- name: CreateApprovalRule :one
INSERT INTO approval_rules (company_id, doc_type, min_amount, created_by, updated_by)
VALUES (@company_id, @doc_type, @min_amount, @actor_id, @actor_id) RETURNING *;

-- name: UpdateApprovalRule :one
UPDATE approval_rules SET doc_type = @doc_type, min_amount = @min_amount, updated_by = @actor_id, version = version + 1
WHERE id = @id AND company_id = @company_id AND version = @version RETURNING *;

-- name: DeleteApprovalRule :execrows
DELETE FROM approval_rules WHERE id = @id AND company_id = @company_id;

-- name: DeleteApprovalRuleSteps :exec
DELETE FROM approval_rule_steps WHERE rule_id = @rule_id;

-- name: InsertApprovalRuleStep :exec
INSERT INTO approval_rule_steps (rule_id, step_no, role_id) VALUES (@rule_id, @step_no, @role_id);

-- name: MatchApprovalRule :one
-- 同類型中 min_amount 最大且不超過單據金額的規則
SELECT id FROM approval_rules
WHERE company_id = @company_id AND doc_type = @doc_type AND min_amount <= @amount::numeric
ORDER BY min_amount DESC LIMIT 1;

-- name: RoleCountInApprovalRules :one
SELECT count(*) FROM approval_rule_steps WHERE role_id = @role_id;

-- name: CompanyRolesByIDs :many
SELECT id, name FROM roles WHERE company_id = @company_id AND id = ANY(@ids::bigint[]) AND is_active;

-- name: InsertDocumentApproval :exec
INSERT INTO document_approvals (company_id, doc_type, doc_id, step_no, role_id, role_name)
VALUES (@company_id, @doc_type, @doc_id, @step_no, sqlc.narg(role_id), @role_name);

-- name: ListDocumentApprovals :many
SELECT d.step_no, d.role_id, d.role_name, d.approver_id, d.approved_at, COALESCE(u.name, '')::text AS approver_name
FROM document_approvals d LEFT JOIN users u ON u.id = d.approver_id
WHERE d.company_id = @company_id AND d.doc_type = @doc_type AND d.doc_id = @doc_id
ORDER BY d.step_no;

-- name: CompleteDocumentApproval :execrows
UPDATE document_approvals SET approver_id = @approver_id, approved_at = now()
WHERE doc_type = @doc_type AND doc_id = @doc_id AND step_no = @step_no AND approver_id IS NULL;

-- name: DeleteDocumentApprovals :exec
DELETE FROM document_approvals WHERE doc_type = @doc_type AND doc_id = @doc_id AND company_id = @company_id;

-- name: UserHasRole :one
SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = @user_id AND role_id = @role_id);
