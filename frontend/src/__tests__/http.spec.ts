import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiRequestError,
  qs,
  request,
  requestPage,
  setAuthHooks,
  type RefreshResult,
} from '@/api/http'

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status })

function mockFetch(...responses: Response[]) {
  const fn = vi.fn<typeof fetch>()
  for (const r of responses) fn.mockResolvedValueOnce(r)
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('request', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setAuthHooks(null)
  })

  it('成功時回傳 data', async () => {
    mockFetch(json(200, { data: { status: 'ok' } }))
    await expect(request('/health')).resolves.toEqual({ status: 'ok' })
  })

  it('失敗時拋出帶錯誤碼與欄位細節的 ApiRequestError', async () => {
    mockFetch(
      json(422, { error: { code: 'SYS-422', message: '輸入資料有誤', details: { name: '必填' } } }),
    )
    const err = await request('/x').catch((e) => e)
    expect(err).toBeInstanceOf(ApiRequestError)
    expect(err).toMatchObject({ status: 422, code: 'SYS-422', details: { name: '必填' } })
  })

  it('非 JSON 回應也能處理', async () => {
    mockFetch(new Response('Bad Gateway', { status: 502 }))
    await expect(request('/health')).rejects.toMatchObject({ code: 'HTTP-502' })
  })

  it('204 回傳 undefined', async () => {
    mockFetch(new Response(null, { status: 204 }))
    await expect(request('/x', { method: 'POST' })).resolves.toBeUndefined()
  })

  it('帶上 access token', async () => {
    const fetch = mockFetch(json(200, { data: 1 }))
    setAuthHooks({
      getToken: () => 'T1',
      refresh: vi.fn<() => Promise<RefreshResult>>(),
      onUnauthorized: vi.fn<() => void>(),
    })
    await request('/x')
    expect(new Headers(fetch.mock.calls[0]![1]!.headers).get('Authorization')).toBe('Bearer T1')
  })

  it('401 時刷新一次並以新 token 重試', async () => {
    let token = 'old'
    const fetch = mockFetch(
      json(401, { error: { code: 'SYS-401', message: 'x' } }),
      json(200, { data: 'ok' }),
    )
    const refresh = vi.fn<() => Promise<RefreshResult>>(async () => {
      token = 'new'
      return 'ok'
    })
    setAuthHooks({ getToken: () => token, refresh, onUnauthorized: vi.fn<() => void>() })

    await expect(request('/system/users')).resolves.toBe('ok')
    expect(refresh).toHaveBeenCalledOnce()
    expect(new Headers(fetch.mock.calls[1]![1]!.headers).get('Authorization')).toBe('Bearer new')
  })

  it('刷新失敗時通知重新登入', async () => {
    mockFetch(json(401, { error: { code: 'SYS-401', message: '請重新登入' } }))
    const onUnauthorized = vi.fn<() => void>()
    setAuthHooks({ getToken: () => 't', refresh: async () => 'expired', onUnauthorized })
    await expect(request('/system/users')).rejects.toMatchObject({ status: 401 })
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('刷新被限流時不登出,回傳 429', async () => {
    mockFetch(json(401, { error: { code: 'SYS-401', message: 'x' } }))
    const onUnauthorized = vi.fn<() => void>()
    setAuthHooks({ getToken: () => 't', refresh: async () => 'throttled', onUnauthorized })
    await expect(request('/system/users')).rejects.toMatchObject({ status: 429, code: 'SYS-429' })
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it('登入請求的 401 不觸發刷新', async () => {
    mockFetch(json(401, { error: { code: 'AUTH-001', message: '帳號或密碼錯誤' } }))
    const refresh = vi.fn<() => Promise<RefreshResult>>()
    setAuthHooks({ getToken: () => 't', refresh, onUnauthorized: vi.fn<() => void>() })
    await expect(request('/auth/login', { method: 'POST', body: '{}' })).rejects.toMatchObject({
      code: 'AUTH-001',
    })
    expect(refresh).not.toHaveBeenCalled()
  })

  it('requestPage 回傳 items 與 meta', async () => {
    mockFetch(json(200, { data: [{ id: 1 }], meta: { page: 2, size: 1, total: 5 } }))
    await expect(requestPage('/x')).resolves.toEqual({
      items: [{ id: 1 }],
      meta: { page: 2, size: 1, total: 5 },
    })
  })
})

describe('qs', () => {
  it('略過空值', () => {
    expect(qs({ a: 1, b: '', c: null, d: undefined, e: false, f: 'x y' })).toBe(
      '?a=1&e=false&f=x+y',
    )
    expect(qs({})).toBe('')
  })
})
