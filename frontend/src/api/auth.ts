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
  /** 公司要求雙因素驗證、但自己還沒啟用:只能先設定 */
  must_setup_2fa: boolean
  two_factor_enabled: boolean
  data_scope: 'all' | 'department' | 'self'
  permissions: string[]
}

export interface Session {
  access_token: string
  expires_at: string
  user: Me
}

/** 已啟用雙因素驗證的帳號,密碼正確後先拿到挑戰憑證,再以驗證碼完成登入 */
export interface TwoFactorChallenge {
  two_factor_required: true
  challenge: string
}

export interface TwoFactorStatus {
  enabled: boolean
  recovery_codes_left: number
  company_requires: boolean
}

export interface TwoFactorSetup {
  secret: string
  otpauth_uri: string
}

export const authApi = {
  login: (username: string, password: string) =>
    http.post<Session | TwoFactorChallenge>('/auth/login', { username, password }),
  loginTwoFactor: (challenge: string, code: string) =>
    http.post<Session>('/auth/login/2fa', { challenge, code }),
  twoFactorStatus: () => http.get<TwoFactorStatus>('/auth/2fa'),
  twoFactorSetup: () => http.post<TwoFactorSetup>('/auth/2fa/setup'),
  twoFactorEnable: (code: string) =>
    http.post<{ recovery_codes: string[] }>('/auth/2fa/enable', { code }),
  twoFactorDisable: (password: string, code: string) =>
    http.post<void>('/auth/2fa/disable', { password, code }),
  recoveryCodes: (password: string, code: string) =>
    http.post<{ recovery_codes: string[] }>('/auth/2fa/recovery-codes', { password, code }),
  refresh: () => http.post<Session>('/auth/refresh'),
  logout: () => http.post<void>('/auth/logout'),
  me: () => http.get<Me>('/auth/me'),
  changePassword: (old_password: string, new_password: string) =>
    http.post<Session>('/auth/change-password', { old_password, new_password }),
}
