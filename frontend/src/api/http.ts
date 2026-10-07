// 對應後端統一回應格式:{ data, meta, error }
export interface ApiError {
  code: string
  message: string
  details?: unknown
}

export interface ApiBody<T> {
  data?: T
  meta?: unknown
  error?: ApiError
}

export class ApiRequestError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })

  let body: ApiBody<T> | undefined
  try {
    body = (await res.json()) as ApiBody<T>
  } catch {
    // 非 JSON 回應(例如 proxy 錯誤頁)
  }

  if (!res.ok || body?.error) {
    throw new ApiRequestError(
      res.status,
      body?.error?.code ?? `HTTP-${res.status}`,
      body?.error?.message ?? '伺服器無回應',
    )
  }
  return body?.data as T
}
