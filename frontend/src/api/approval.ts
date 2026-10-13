import { http, qs } from './http'
import type { Decimal } from './masterdata'

export interface DocTypeOption {
  key: string
  label: string
}

/** 簽法:any 或簽(任一角色核准即過)/ all 會簽(每個角色都要核准) */
export type ApprovalMode = 'any' | 'all'

export interface RuleStep {
  step: number
  mode: ApprovalMode
  roles: { role_id: number; role_name: string }[]
}

export interface ApprovalRule {
  id: number
  doc_type: string
  doc_label: string
  min_amount: Decimal
  /** 限定送審者所屬部門(含下層部門);null = 全公司 */
  department_id: number | null
  department_name: string
  steps: RuleStep[]
  version: number
}

export interface ApprovalRuleInput {
  doc_type: string
  min_amount: string
  department_id: number | null
  /** 第 2 層起的各層:一或多個角色與簽法 */
  steps: { role_ids: number[]; mode: ApprovalMode }[]
  version?: number
}

export interface ProgressStep {
  step: number
  slot: number
  mode: ApprovalMode
  /** 或簽層已由其他角色通過,這一格免簽 */
  waived: boolean
  role_name: string
  approver_name: string
  /** 代理核准時的委託人姓名;空字串 = 本人核准 */
  acting_for: string
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

export interface InboxRow {
  doc_type: string
  doc_label: string
  doc_id: number
  doc_no: string
  doc_date: string
  partner_name: string
  currency: string
  amount: Decimal
  submitted_at: string | null
  step: number
  total_steps: number
  step_role: string
}

/** 單據類型 → 單據編輯頁的路由名稱 */
export const inboxRoutes: Record<string, string> = {
  purchase_order: 'purchase-order',
  goods_receipt: 'purchase-receipt',
  sales_order: 'sales-order',
  delivery: 'sales-delivery',
  collection: 'collection',
  payment: 'payment',
  work_order: 'work-order',
  subcontract_order: 'subcontract',
  stock_document: 'inventory-document',
}

export const inboxApi = {
  list: () => http.get<InboxRow[]>('/approval/inbox'),
}

export interface Delegation {
  id: number
  delegator_id: number
  delegator_name: string
  delegate_id: number
  delegate_name: string
  valid_from: string
  valid_to: string
  note: string
}

export interface DelegationInput {
  delegator_id?: number
  delegate_id: number
  valid_from: string
  valid_to: string
  note?: string
}

export const delegationApi = {
  list: (all = false) => http.get<Delegation[]>(`/approval/delegations${all ? '?scope=all' : ''}`),
  create: (input: DelegationInput) => http.post<Delegation>('/approval/delegations', input),
  remove: (id: number) => http.delete(`/approval/delegations/${id}`),
}
