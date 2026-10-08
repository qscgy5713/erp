import { http, qs } from './http'
import type { Decimal } from './masterdata'

export interface DocTypeOption {
  key: string
  label: string
}

export interface RuleStep {
  step: number
  role_id: number | null
  role_name: string
}

export interface ApprovalRule {
  id: number
  doc_type: string
  doc_label: string
  min_amount: Decimal
  steps: RuleStep[]
  version: number
}

export interface ApprovalRuleInput {
  doc_type: string
  min_amount: string
  role_ids: number[]
  version?: number
}

export interface ProgressStep {
  step: number
  role_name: string
  approver_name: string
  approved_at: string | null
  done: boolean
}

export interface ApprovalProgress {
  required: number
  steps: ProgressStep[]
  current: number
  can_approve: boolean
}

export const approvalApi = {
  docTypes: () => http.get<DocTypeOption[]>('/approval/doc-types'),
  rules: () => http.get<ApprovalRule[]>('/approval/rules'),
  createRule: (input: ApprovalRuleInput) => http.post<ApprovalRule>('/approval/rules', input),
  updateRule: (id: number, input: ApprovalRuleInput) =>
    http.put<ApprovalRule>(`/approval/rules/${id}`, input),
  deleteRule: (id: number) => http.delete(`/approval/rules/${id}`),
  progress: (doc_type: string, doc_id: number) =>
    http.get<ApprovalProgress>(`/approval/progress${qs({ doc_type, doc_id })}`),
}
