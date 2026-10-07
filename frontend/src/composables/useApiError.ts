import { ref } from 'vue'
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

  return { fieldErrors, handle, reset }
}
