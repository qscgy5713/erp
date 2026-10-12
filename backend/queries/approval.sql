-- ======== 多層簽核(D61) ========

-- name: ListApprovalRules :many
SELECT * FROM approval_rules WHERE company_id = @company_id ORDER BY doc_type, min_amount;

-- name: ListApprovalRuleSteps :many
SELECT s.rule_id, s.step_no, s.mode, s.role_id, r.name AS role_name
FROM approval_rule_steps s JOIN roles r ON r.id = s.role_id
JOIN approval_rules ar ON ar.id = s.rule_id
WHERE ar.company_id = @company_id ORDER BY s.rule_id, s.step_no, s.role_id;

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
INSERT INTO approval_rule_steps (rule_id, step_no, mode, role_id) VALUES (@rule_id, @step_no, @mode, @role_id);

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
INSERT INTO document_approvals (company_id, doc_type, doc_id, step_no, slot_no, mode, role_id, role_name)
VALUES (@company_id, @doc_type, @doc_id, @step_no, @slot_no, @mode, sqlc.narg(role_id), @role_name);

-- name: ListDocumentApprovals :many
SELECT d.step_no, d.slot_no, d.mode, d.role_id, d.role_name, d.approver_id, d.approved_at, d.acting_for, COALESCE(u.name, '')::text AS approver_name,
       COALESCE(af.name, '')::text AS acting_for_name
FROM document_approvals d LEFT JOIN users u ON u.id = d.approver_id LEFT JOIN users af ON af.id = d.acting_for
WHERE d.company_id = @company_id AND d.doc_type = @doc_type AND d.doc_id = @doc_id
ORDER BY d.step_no, d.slot_no;

-- name: CompleteDocumentApproval :execrows
UPDATE document_approvals SET approver_id = @approver_id, approved_at = now(), acting_for = sqlc.narg(acting_for)
WHERE doc_type = @doc_type AND doc_id = @doc_id AND step_no = @step_no AND slot_no = @slot_no AND approver_id IS NULL;

-- name: DeleteDocumentApprovals :exec
DELETE FROM document_approvals WHERE doc_type = @doc_type AND doc_id = @doc_id AND company_id = @company_id;

-- name: UserHasRole :one
SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = @user_id AND role_id = @role_id);

-- name: ApprovalDocVisible :one
-- 單據是否存在於本公司,且在使用者的資料範圍內(業務類單據依負責業務快照,與各單據頁相同的規則)
SELECT (
  (@doc_type::text = 'purchase_order' AND EXISTS (SELECT 1 FROM purchase_orders x WHERE x.id = @doc_id AND x.company_id = @company_id))
  OR (@doc_type::text = 'goods_receipt' AND EXISTS (SELECT 1 FROM goods_receipts x WHERE x.id = @doc_id AND x.company_id = @company_id))
  OR (@doc_type::text = 'stock_document' AND EXISTS (SELECT 1 FROM stock_documents x WHERE x.id = @doc_id AND x.company_id = @company_id))
  OR (@doc_type::text = 'work_order' AND EXISTS (SELECT 1 FROM work_orders x WHERE x.id = @doc_id AND x.company_id = @company_id))
  OR (@doc_type::text = 'payment' AND EXISTS (SELECT 1 FROM settlements x WHERE x.id = @doc_id AND x.company_id = @company_id AND x.side = 'payment'))
  OR (@doc_type::text = 'sales_order' AND EXISTS (
        SELECT 1 FROM sales_orders x LEFT JOIN users su ON su.id = x.sales_user_id
        WHERE x.id = @doc_id AND x.company_id = @company_id
          AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
          AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))))
  OR (@doc_type::text = 'delivery' AND EXISTS (
        SELECT 1 FROM deliveries x LEFT JOIN users su ON su.id = x.sales_user_id
        WHERE x.id = @doc_id AND x.company_id = @company_id
          AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
          AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))))
  OR (@doc_type::text = 'collection' AND EXISTS (
        SELECT 1 FROM settlements x LEFT JOIN users su ON su.id = x.sales_user_id
        WHERE x.id = @doc_id AND x.company_id = @company_id AND x.side = 'receipt'
          AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
          AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))))
)::boolean AS visible;

-- name: PendingApprovalDocs :many
-- 待核准清單(待我核准收件匣):各類單據中 status = 'pending' 的單據;只含呼叫者有核准權限的類型(@types),
-- 業務類單據套用資料範圍。是否輪到「我」由程式依簽核流程判斷。
SELECT doc_type, doc_id, doc_no, doc_date, partner_name, currency, amount, submitted_at FROM (
    SELECT 'purchase_order'::text AS doc_type, x.id AS doc_id, x.doc_no, x.doc_date, s.name AS partner_name,
           x.currency::text AS currency, x.total_amount AS amount, x.submitted_at
    FROM purchase_orders x JOIN suppliers s ON s.id = x.supplier_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'purchase_order' = ANY(@types::text[])
  UNION ALL
    SELECT 'goods_receipt', x.id, x.doc_no, x.doc_date, s.name, x.currency::text, x.total_amount, x.submitted_at
    FROM goods_receipts x JOIN suppliers s ON s.id = x.supplier_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'goods_receipt' = ANY(@types::text[])
  UNION ALL
    SELECT 'sales_order', x.id, x.doc_no, x.doc_date, c.name, x.currency::text, x.total_amount, x.submitted_at
    FROM sales_orders x JOIN customers c ON c.id = x.customer_id LEFT JOIN users su ON su.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'sales_order' = ANY(@types::text[])
      AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
      AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
  UNION ALL
    SELECT 'delivery', x.id, x.doc_no, x.doc_date, c.name, x.currency::text, x.total_amount, x.submitted_at
    FROM deliveries x JOIN customers c ON c.id = x.customer_id LEFT JOIN users su ON su.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'delivery' = ANY(@types::text[])
      AND (sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
      AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))
  UNION ALL
    SELECT CASE x.side WHEN 'receipt' THEN 'collection' ELSE 'payment' END, x.id, x.doc_no, x.doc_date,
           COALESCE(c.name, sp.name), x.currency::text, x.amount, x.submitted_at
    FROM settlements x LEFT JOIN customers c ON c.id = x.customer_id LEFT JOIN suppliers sp ON sp.id = x.supplier_id
         LEFT JOIN users su ON su.id = x.sales_user_id
    WHERE x.company_id = @company_id AND x.status = 'pending'
      AND (CASE x.side WHEN 'receipt' THEN 'collection' ELSE 'payment' END) = ANY(@types::text[])
      AND (x.side = 'payment'
           OR ((sqlc.narg(scope_user_id)::bigint IS NULL OR x.sales_user_id = sqlc.narg(scope_user_id))
               AND (sqlc.narg(scope_dept_id)::bigint IS NULL OR su.department_id = sqlc.narg(scope_dept_id))))
  UNION ALL
    SELECT 'work_order', x.id, x.doc_no, x.doc_date, i.name, 'TWD', x.processing_cost, x.submitted_at
    FROM work_orders x JOIN items i ON i.id = x.item_id
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'work_order' = ANY(@types::text[])
  UNION ALL
    SELECT 'stock_document', x.id, x.doc_no, x.doc_date,
           CASE x.doc_type WHEN 'adjustment' THEN '庫存調整' WHEN 'transfer' THEN '庫存調撥' ELSE '庫存盤點' END,
           'TWD', 0::numeric, x.submitted_at
    FROM stock_documents x
    WHERE x.company_id = @company_id AND x.status = 'pending' AND 'stock_document' = ANY(@types::text[])
) t
ORDER BY submitted_at NULLS LAST, doc_no
LIMIT 300;

-- name: DocumentApprovalsForDocs :many
SELECT d.doc_type, d.doc_id, d.step_no, d.slot_no, d.mode, d.role_id, d.role_name, d.approver_id, d.acting_for
FROM document_approvals d
WHERE d.company_id = @company_id
  AND d.doc_type = ANY(@types::text[]) AND d.doc_id = ANY(@ids::bigint[]) -- 超集合,程式再依 (類型, id) 配對
ORDER BY d.doc_type, d.doc_id, d.step_no, d.slot_no;

-- name: UserRoleIDs :many
SELECT role_id FROM user_roles WHERE user_id = @user_id;

-- name: UserRoleIDsFor :many
SELECT DISTINCT role_id FROM user_roles WHERE user_id = ANY(@user_ids::bigint[]);

-- ======== 簽核代理人(D87) ========

-- name: ActiveDelegators :many
-- 今天有效、委託我代理的人(委託人須仍啟用)
SELECT DISTINCT d.delegator_id FROM approval_delegations d JOIN users u ON u.id = d.delegator_id AND u.is_active
WHERE d.company_id = @company_id AND d.delegate_id = @delegate_id
  AND CURRENT_DATE BETWEEN d.valid_from AND d.valid_to;

-- name: ActiveDelegatedPerms :many
-- 委託人的「核准」類權限(角色須啟用)
SELECT DISTINCT d.delegator_id, rp.permission
FROM approval_delegations d
JOIN users u ON u.id = d.delegator_id AND u.is_active
JOIN user_roles ur ON ur.user_id = d.delegator_id
JOIN roles r ON r.id = ur.role_id AND r.is_active
JOIN role_permissions rp ON rp.role_id = r.id AND rp.permission LIKE '%.approve'
WHERE d.company_id = @company_id AND d.delegate_id = @delegate_id
  AND CURRENT_DATE BETWEEN d.valid_from AND d.valid_to;

-- name: ListDelegations :many
SELECT d.id, d.delegator_id, du.name AS delegator_name, d.delegate_id, eu.name AS delegate_name,
       d.valid_from, d.valid_to, d.note
FROM approval_delegations d
JOIN users du ON du.id = d.delegator_id JOIN users eu ON eu.id = d.delegate_id
WHERE d.company_id = @company_id
  AND d.valid_to >= CURRENT_DATE - 30
  AND (sqlc.narg(user_id)::bigint IS NULL OR d.delegator_id = sqlc.narg(user_id) OR d.delegate_id = sqlc.narg(user_id))
ORDER BY d.valid_from DESC, d.id DESC;

-- name: GetDelegation :one
SELECT * FROM approval_delegations WHERE id = @id AND company_id = @company_id;

-- name: CreateDelegation :one
INSERT INTO approval_delegations (company_id, delegator_id, delegate_id, valid_from, valid_to, note, created_by)
VALUES (@company_id, @delegator_id, @delegate_id, @valid_from, @valid_to, @note, @actor_id) RETURNING *;

-- name: DeleteDelegation :execrows
DELETE FROM approval_delegations WHERE id = @id AND company_id = @company_id;

-- name: ActiveUserInCompany :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = @id AND company_id = @company_id AND is_active);

-- name: RoleActiveHolderCount :one
-- 角色目前有幾位啟用中的使用者(排除指定使用者);會簽時用來優先填「別人較難填」的簽核格
SELECT count(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id AND u.is_active
WHERE ur.role_id = @role_id AND ur.user_id <> @except_user_id;
