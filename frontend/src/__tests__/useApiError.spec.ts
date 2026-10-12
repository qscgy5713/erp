import { describe, expect, it } from 'vitest'
import { nextTick, reactive } from 'vue'
import { useApiError } from '@/composables/useApiError'

describe('useApiError.autoClear', () => {
  it('修改欄位後即時清除該欄位(含子欄位)的錯誤,其他欄位的錯誤保留', async () => {
    const form = reactive({ name: 'a', code: 'b', lines: [{ qty: '1' }] })
    const { fieldErrors, autoClear } = useApiError()
    autoClear(form)
    fieldErrors.value = { name: '必填', code: '重複', 'lines.0': '數量錯誤', unknown: '其他' }

    form.name = 'x'
    await nextTick()
    expect(fieldErrors.value).toEqual({ code: '重複', 'lines.0': '數量錯誤', unknown: '其他' })

    form.lines[0]!.qty = '2'
    await nextTick()
    expect(fieldErrors.value).toEqual({ code: '重複', unknown: '其他' })

    form.code = 'b' // 值沒變,不清除
    await nextTick()
    expect(fieldErrors.value).toEqual({ code: '重複', unknown: '其他' })
  })
})
