import { http, qs, requestPage } from './http'

export interface Health {
  status: string
}

export const getHealth = () => http.get<Health>('/health')

export type DataScope = 'all' | 'department' | 'self'

export interface Department {
  id: number
  parent_id: number | null
  code: string
  name: string
  sort_order: number
  is_active: boolean
  user_count: number
  version: number
  updated_at: string
}

export interface DepartmentInput {
  parent_id: number | null
  code: string
  name: string
  sort_order: number
  is_active?: boolean
  version?: number
}

export interface RoleRef {
  id: number
  name: string
}

export interface User {
  id: number
  username: string
  name: string
  email: string | null
  department_id: number | null
  department_name?: string | null
  is_superadmin: boolean
  is_active: boolean
  must_change_password: boolean
  locked_until: string | null
  last_login_at: string | null
  roles: RoleRef[]
  role_ids: number[]
  version: number
  created_at: string
  updated_at: string
}

export interface UserQuery {
  keyword?: string
  department_id?: number | null
  is_active?: boolean | null
  page: number
  size: number
}

export interface CreateUserInput {
  username: string
  name: string
  email: string | null
  department_id: number | null
  password: string
  role_ids: number[]
}

export interface UpdateUserInput {
  name: string
  email: string | null
  department_id: number | null
  is_active: boolean
  role_ids: number[]
  version: number
}

export interface Role {
  id: number
  code: string
  name: string
  description: string
  data_scope: DataScope
  is_active: boolean
  user_count: number
  permissions?: string[]
  version: number
  updated_at: string
}

export interface RoleInput {
  code: string
  name: string
  description: string
  data_scope: DataScope
  permissions: string[]
  is_active?: boolean
  version?: number
}

export interface PermissionGroup {
  module: string
  resource: string
  permissions: { code: string; name: string }[]
}

export interface AuditLog {
  id: number
  user_id: number | null
  username: string | null
  user_name: string | null
  action: string
  entity_type: string
  entity_id: number | null
  summary: string
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  ip: string
  user_agent: string
  request_id: string
  created_at: string
}

export interface AuditQuery {
  entity_type?: string
  action?: string
  user_id?: number | null
  from?: string
  to?: string
  page: number
  size: number
}

export interface DocNumberRule {
  doc_type: string
  name: string
  prefix: string
  date_format: 'YYYYMMDD' | 'YYYYMM' | 'YYYY' | 'NONE'
  seq_length: number
  example: string
  version: number
  updated_at: string
}

export interface Company {
  name: string
  tax_id: string
  tax_reg_no: string
  version: number
}

export const systemApi = {
  company: () => http.get<Company>('/system/company'),
  updateCompany: (input: Omit<Company, 'version'> & { version: number }) =>
    http.put<Company>('/system/company', input),
  permissions: () => http.get<PermissionGroup[]>('/system/permissions'),

  departments: () => http.get<Department[]>('/system/departments'),
  createDepartment: (input: DepartmentInput) => http.post<Department>('/system/departments', input),
  updateDepartment: (id: number, input: DepartmentInput) =>
    http.put<Department>(`/system/departments/${id}`, input),

  users: (q: UserQuery) => requestPage<User>(`/system/users${qs({ ...q })}`),
  user: (id: number) => http.get<User>(`/system/users/${id}`),
  createUser: (input: CreateUserInput) => http.post<User>('/system/users', input),
  updateUser: (id: number, input: UpdateUserInput) => http.put<User>(`/system/users/${id}`, input),
  resetPassword: (id: number, password: string) =>
    http.post<void>(`/system/users/${id}/reset-password`, { password }),
  unlockUser: (id: number) => http.post<void>(`/system/users/${id}/unlock`),

  roles: () => http.get<Role[]>('/system/roles'),
  role: (id: number) => http.get<Role>(`/system/roles/${id}`),
  createRole: (input: RoleInput) => http.post<Role>('/system/roles', input),
  updateRole: (id: number, input: RoleInput) => http.put<Role>(`/system/roles/${id}`, input),
  deleteRole: (id: number) => http.delete(`/system/roles/${id}`),

  auditLogs: (q: AuditQuery) => requestPage<AuditLog>(`/system/audit-logs${qs({ ...q })}`),

  docNumberRules: () => http.get<DocNumberRule[]>('/system/doc-number-rules'),
  updateDocNumberRule: (
    docType: string,
    input: Omit<DocNumberRule, 'doc_type' | 'example' | 'updated_at'>,
  ) => http.put<DocNumberRule>(`/system/doc-number-rules/${docType}`, input),
}
