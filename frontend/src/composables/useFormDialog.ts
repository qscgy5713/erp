import { reactive, ref, type Ref } from 'vue'
import { ElMessage, type FormInstance } from 'element-plus'
import { useApiError } from './useApiError'

interface Versioned {
  id: number
  version: number
}

/**
 * 新增/編輯對話框的共用狀態:開啟時載入資料、儲存時驗證並帶上 version(樂觀鎖)。
 * save 回傳的 Promise 完成後會關閉對話框並呼叫 onSaved。
 */
export function useFormDialog<F extends object, T extends Versioned>(opts: {
  defaults: () => F
  fromRow: (row: T) => F
  create: (form: F) => Promise<unknown>
  update: (id: number, form: F & { version: number }) => Promise<unknown>
  onSaved: () => void | Promise<void>
}) {
  const visible = ref(false)
  const saving = ref(false)
  const editing = ref(null) as Ref<T | null>
  const formRef = ref<FormInstance>()
  const form = reactive(opts.defaults()) as F
  const { fieldErrors, handle, reset } = useApiError()

  function openCreate(overrides: Partial<F> = {}) {
    editing.value = null
    Object.assign(form, opts.defaults(), overrides)
    reset()
    visible.value = true
    formRef.value?.clearValidate()
  }

  function openEdit(row: T) {
    editing.value = row
    Object.assign(form, opts.fromRow(row))
    reset()
    visible.value = true
    formRef.value?.clearValidate()
  }

  async function save() {
    if (!(await formRef.value?.validate().catch(() => false))) return
    saving.value = true
    try {
      if (editing.value) {
        await opts.update(editing.value.id, { ...form, version: editing.value.version })
      } else {
        await opts.create({ ...form })
      }
      ElMessage.success('已儲存')
      reset()
      visible.value = false
      await opts.onSaved()
    } catch (e) {
      handle(e)
    } finally {
      saving.value = false
    }
  }

  return {
    visible,
    saving,
    editing,
    formRef,
    form,
    fieldErrors,
    openCreate,
    openEdit,
    save,
    handleError: handle,
  }
}
