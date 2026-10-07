import { http } from './http'

export interface Me {
  id: number
  company_id: number
  username: string
  name: string
  email: string | null
  department_id: number | null
  is_superadmin: boolean
  must_change_password: boolean
  data_scope: 'all' | 'department' | 'self'
  permissions: string[]
}

export interface Session {
  access_token: string
  expires_at: string
  user: Me
}

export const authApi = {
  login: (username: string, password: string) =>
    http.post<Session>('/auth/login', { username, password }),
  refresh: () => http.post<Session>('/auth/refresh'),
  logout: () => http.post<void>('/auth/logout'),
  me: () => http.get<Me>('/auth/me'),
  changePassword: (old_password: string, new_password: string) =>
    http.post<Session>('/auth/change-password', { old_password, new_password }),
}
