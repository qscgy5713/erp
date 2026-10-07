// 對應後端統一回應格式:{ data, meta, error }
export interface ApiError {
  code: string
  message: string
  details?: Record<string, string>
}

export interface ApiBody<T> {
  data?: T
  meta?: PageMeta
  error?: ApiError
}

export interface PageMeta {
  page: number
  size: number
  total: number
}

export interface Paged<T> {
  items: T[]
  meta: PageMeta
}

export class ApiRequestError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, string>,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

// 由 auth store 註冊,避免 http 與 store 互相 import
/** ok:已換發;expired:需重新登入;throttled:被限流,登入狀態仍有效,稍後再試 */
export type RefreshResult = 'ok' | 'expired' | 'throttled'

export interface AuthHooks {
  getToken(): string | null
  /** 嘗試以 refresh cookie 換發 token */
  refresh(): Promise<RefreshResult>
  /** 刷新失敗,需要重新登入 */
  onUnauthorized(): void
}

let hooks: AuthHooks | null = null

// 這些端點本身就是登入/刷新流程,401 時不再嘗試刷新,避免無限迴圈
const noRetryPaths = new Set(['/auth/login', '/auth/refresh', '/auth/logout'])

export function setAuthHooks(h: AuthHooks | null) {
  hooks = h
}

async function send<T>(path: string, init: RequestInit = {}, retry = true): Promise<ApiBody<T>> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined) headers.set('Content-Type', 'application/json')
  const token = hooks?.getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'same-origin' })

  // access token 過期:刷新一次後重試
  if (res.status === 401 && retry && hooks && token && !noRetryPaths.has(path)) {
    const result = await hooks.refresh()
    if (result === 'ok') return send<T>(path, init, false)
    // 被限流不代表登入失效,不要把使用者登出
    if (result === 'throttled') throw new ApiRequestError(429, 'SYS-429', '請求過於頻繁,請稍後再試')
    hooks.onUnauthorized()
  }

  let body: ApiBody<T> = {}
  if (res.status !== 204) {
    try {
      body = (await res.json()) as ApiBody<T>
    } catch {
      // 非 JSON 回應(例如 proxy 錯誤頁)
    }
  }
  if (!res.ok || body.error) {
    throw new ApiRequestError(
      res.status,
      body.error?.code ?? `HTTP-${res.status}`,
      body.error?.message ?? '伺服器無回應',
      body.error?.details,
    )
  }
  return body
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  return (await send<T>(path, init)).data as T
}

export async function requestPage<T>(path: string): Promise<Paged<T>> {
  const body = await send<T[]>(path)
  return { items: body.data ?? [], meta: body.meta ?? { page: 1, size: 0, total: 0 } }
}

export const http = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'POST',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  put: <T>(path: string, body: unknown) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
  delete: <T = void>(path: string) => request<T>(path, { method: 'DELETE' }),
}

/** 組查詢字串,略過空值 */
export function qs(params: Record<string, string | number | boolean | null | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== null && v !== undefined && v !== '') sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}
