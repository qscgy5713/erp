import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ApiRequestError } from '@/api/http'

/** 統一處理 API 錯誤:欄位錯誤顯示在表單上,其他錯誤跳訊息 */
export function useApiError() {
  const fieldErrors = ref<Record<string, string>>({})

  function handle(e: unknown) {
    fieldErrors.value = {}
    if (e instanceof ApiRequestError) {
      if (e.details && e.code === 'SYS-422') {
        fieldErrors.value = { ...e.details }
      }
      ElMessage.error(e.message)
      return
    }
    ElMessage.error('發生未預期的錯誤')
    console.error(e)
  }

  function reset() {
    fieldErrors.value = {}
  }

  /** 表單欄位被修改後,即時清掉該欄位(含 `lines.0` 這類子欄位)的伺服器錯誤,不必等下次儲存 */
  function autoClear(form: object) {
    const snap = () =>
      Object.fromEntries(Object.entries(form).map(([k, v]) => [k, JSON.stringify(v)]))
    let prev = snap()
    watch(
      () => form,
      () => {
        const next = snap()
        const keys = Object.keys(fieldErrors.value)
        if (keys.length) {
          const kept: Record<string, string> = {}
          for (const k of keys) {
            const top = k.split(/[.[]/)[0]!
            if (!(top in next) || prev[top] === next[top]) kept[k] = fieldErrors.value[k]!
          }
          if (Object.keys(kept).length !== keys.length) fieldErrors.value = kept
        }
        prev = next
      },
      { deep: true },
    )
  }

  return { fieldErrors, handle, reset, autoClear }
}
