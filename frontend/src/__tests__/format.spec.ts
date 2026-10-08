import { describe, expect, it } from 'vitest'
import { dataScopeLabels, formatDateTime } from '@/utils/format'

describe('formatDateTime', () => {
  it('後端 UTC 時間轉成台灣時間顯示(+8)', () => {
    // 2026-10-08 16:30:05 UTC → 台灣 2026-10-09 00:30:05(跨日)
    const s = formatDateTime('2026-10-08T16:30:05Z')
    expect(s).toContain('2026')
    expect(s).toMatch(/10.*09/)
    expect(s).toMatch(/00:30:05/)
  })

  it('空值與非法日期顯示為空字串', () => {
    expect(formatDateTime(null)).toBe('')
    expect(formatDateTime(undefined)).toBe('')
    expect(formatDateTime('')).toBe('')
    expect(formatDateTime('not-a-date')).toBe('')
  })
})

describe('dataScopeLabels', () => {
  it('涵蓋三種資料範圍', () => {
    expect(Object.keys(dataScopeLabels).sort()).toEqual(['all', 'department', 'self'])
  })
})
