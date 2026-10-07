import type { FormItemRule } from 'element-plus'

// blur 給輸入框、change 給下拉選單(只用 blur 時,選了值錯誤訊息仍會殘留)
export const required: FormItemRule = {
  required: true,
  message: '必填',
  trigger: ['blur', 'change'],
}

/** 非負小數,最多 places 位小數(值以字串保存) */
export function decimalRule(places: number, opts: { positive?: boolean } = {}): FormItemRule {
  const re = new RegExp(`^\\d+(\\.\\d{1,${places}})?$`)
  return {
    trigger: 'blur',
    validator: (_r, v: string, cb) => {
      const s = String(v ?? '').trim()
      if (s === '') return cb()
      if (!re.test(s)) return cb(new Error(`須為數字,最多 ${places} 位小數`))
      if (opts.positive && Number(s) <= 0) return cb(new Error('須大於 0'))
      cb()
    },
  }
}

/** 台灣統一編號檢查碼(與後端 taxid.Valid 相同規則) */
export function validTaxId(s: string): boolean {
  if (!/^\d{8}$/.test(s)) return false
  const w = [1, 2, 1, 2, 1, 2, 4, 1]
  let sum = 0
  for (let i = 0; i < 8; i++) {
    const p = Number(s[i]) * w[i]!
    sum += Math.floor(p / 10) + (p % 10)
  }
  return sum % 5 === 0 || (s[6] === '7' && (sum + 1) % 5 === 0)
}

export const taxIdRule: FormItemRule = {
  trigger: 'blur',
  validator: (_r, v: string | null, cb) =>
    !v || validTaxId(v.trim()) ? cb() : cb(new Error('統一編號格式或檢查碼錯誤')),
}
