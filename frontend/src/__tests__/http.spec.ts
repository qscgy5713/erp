import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiRequestError, request } from '@/api/http'

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status })))
}

describe('request', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('成功時回傳 data', async () => {
    mockFetch(200, { data: { status: 'ok' } })
    await expect(request('/health')).resolves.toEqual({ status: 'ok' })
  })

  it('失敗時拋出帶錯誤碼的 ApiRequestError', async () => {
    mockFetch(503, { error: { code: 'SYS-503', message: '資料庫無回應' } })
    const err = await request('/health').catch((e) => e)
    expect(err).toBeInstanceOf(ApiRequestError)
    expect(err).toMatchObject({ status: 503, code: 'SYS-503', message: '資料庫無回應' })
  })

  it('非 JSON 回應也能處理', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('Bad Gateway', { status: 502 })))
    await expect(request('/health')).rejects.toMatchObject({ code: 'HTTP-502' })
  })
})
