import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import type { Me } from '@/api/auth'

const me = (over: Partial<Me> = {}): Me => ({
  id: 1,
  company_id: 1,
  username: 'u',
  name: 'U',
  email: null,
  department_id: null,
  is_superadmin: false,
  must_change_password: false,
  data_scope: 'self',
  permissions: ['system.user.read'],
  ...over,
})

describe('auth store', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('can 依權限判斷,超級管理員全部允許', () => {
    const auth = useAuthStore()
    expect(auth.can('system.user.read')).toBe(false) // 未登入
    auth.user = me()
    expect(auth.can(undefined)).toBe(true)
    expect(auth.can('system.user.read')).toBe(true)
    expect(auth.can(['system.role.read', 'system.user.read'])).toBe(true)
    expect(auth.can('system.role.read')).toBe(false)
    auth.user = me({ is_superadmin: true, permissions: [] })
    expect(auth.can('anything')).toBe(true)
  })

  it('同時多次刷新只打一次 API', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ data: { access_token: 'T', expires_at: '', user: me() } }), {
        status: 200,
      }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const auth = useAuthStore()
    const results = await Promise.all([auth.refresh(), auth.refresh(), auth.refresh()])
    expect(results).toEqual(['ok', 'ok', 'ok'])
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(auth.token).toBe('T')
    expect(auth.isLoggedIn).toBe(true)
  })

  it('刷新失敗時清除登入狀態', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: 'AUTH-004', message: 'x' } }), {
          status: 401,
        }),
      ),
    )
    const auth = useAuthStore()
    auth.token = 'old'
    auth.user = me()
    expect(await auth.refresh()).toBe('expired')
    expect(auth.token).toBeNull()
    expect(auth.isLoggedIn).toBe(false)
  })

  it('刷新被限流(429)時保留登入狀態', async () => {
    const body = JSON.stringify({ error: { code: 'SYS-429', message: 'x' } })
    const fetchMock = vi.fn<typeof fetch>()
    fetchMock.mockResolvedValue(new Response(body, { status: 429 }))
    vi.stubGlobal('fetch', fetchMock)
    const auth = useAuthStore()
    auth.token = 'old'
    auth.user = me()
    expect(await auth.refresh()).toBe('throttled')
    expect(auth.token).toBe('old')
    expect(auth.isLoggedIn).toBe(true)
  })
})
