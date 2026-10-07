import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi, type Me, type Session } from '@/api/auth'
import { ApiRequestError, setAuthHooks, type RefreshResult } from '@/api/http'

export const useAuthStore = defineStore('auth', () => {
  // access token 只放記憶體,不寫入 localStorage(避免 XSS 竊取);
  // 重新整理頁面時以 HttpOnly refresh cookie 恢復登入
  const token = ref<string | null>(null)
  const user = ref<Me | null>(null)
  const initialized = ref(false)

  const isLoggedIn = computed(() => user.value !== null)
  const mustChangePassword = computed(() => user.value?.must_change_password ?? false)

  function can(perms: string | string[] | undefined): boolean {
    if (!perms || (Array.isArray(perms) && perms.length === 0)) return true
    if (!user.value) return false
    if (user.value.is_superadmin) return true
    const list = Array.isArray(perms) ? perms : [perms]
    return list.some((p) => user.value!.permissions.includes(p))
  }

  function apply(s: Session) {
    token.value = s.access_token
    user.value = s.user
  }

  function clear() {
    token.value = null
    user.value = null
  }

  let refreshing: Promise<RefreshResult> | null = null

  /** 單一飛行:同時多個 401 只刷新一次;跨分頁用 Web Locks 排隊,避免同時輪替被誤判為重放 */
  function refresh(): Promise<RefreshResult> {
    refreshing ??= withLock(async (): Promise<RefreshResult> => {
      try {
        apply(await authApi.refresh())
        return 'ok'
      } catch (e) {
        // 429 只是請求太頻繁,refresh cookie 仍有效,保留登入狀態
        if (e instanceof ApiRequestError && e.status === 429) return 'throttled'
        clear()
        return 'expired'
      }
    }).finally(() => {
      refreshing = null
    })
    return refreshing
  }

  /** 啟動時嘗試以 cookie 恢復登入,只做一次 */
  async function init() {
    if (initialized.value) return
    await refresh()
    initialized.value = true
  }

  async function login(username: string, password: string) {
    apply(await authApi.login(username, password))
    initialized.value = true
  }

  async function logout() {
    try {
      await authApi.logout()
    } finally {
      clear()
    }
  }

  async function changePassword(oldPw: string, newPw: string) {
    apply(await authApi.changePassword(oldPw, newPw))
  }

  return {
    token,
    user,
    initialized,
    isLoggedIn,
    mustChangePassword,
    can,
    init,
    login,
    logout,
    refresh,
    changePassword,
    clear,
  }
})

async function withLock<T>(fn: () => Promise<T>): Promise<T> {
  if (typeof navigator !== 'undefined' && navigator.locks) {
    return navigator.locks.request('erp-auth-refresh', fn)
  }
  return fn()
}

/** 在 app 啟動時呼叫,把 store 接到 http client */
export function installAuthHooks(onUnauthorized: () => void) {
  const auth = useAuthStore()
  setAuthHooks({
    getToken: () => auth.token,
    refresh: () => auth.refresh(),
    onUnauthorized: () => {
      auth.clear()
      onUnauthorized()
    },
  })
}
