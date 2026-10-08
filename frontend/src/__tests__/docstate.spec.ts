import { describe, expect, it } from 'vitest'
import { allowedActions } from '@/utils/docstate'

// 須與後端 internal/shared/docstate 的 transitions 一致,否則會顯示按了會失敗的按鈕。
// 結案 / 重開(close / reopen)庫存單據與進貨單不使用,只有採購單使用。
describe('allowedActions', () => {
  it.each([
    ['draft', ['submit', 'void']],
    ['pending', ['approve', 'reject', 'void']],
    ['approved', ['post', 'unapprove', 'void']],
    ['posted', ['unpost']],
    ['voided', []],
  ] as const)('%s → %j', (status, want) => {
    expect(allowedActions(status).sort()).toEqual([...want].sort())
  })

  // 採購單:須與後端 internal/purchase orderTransition 一致
  it.each([
    ['draft', ['submit', 'void']],
    ['pending', ['approve', 'reject', 'void']],
    ['approved', ['unapprove', 'void', 'close']],
    ['posted', []],
    ['closed', ['reopen']],
    ['voided', []],
  ] as const)('採購單 %s → %j', (status, want) => {
    expect(allowedActions(status, 'order').sort()).toEqual([...want].sort())
  })
})
