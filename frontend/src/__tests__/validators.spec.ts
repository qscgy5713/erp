import { describe, expect, it } from 'vitest'
import { validTaxId } from '@/utils/validators'

describe('validTaxId(與後端測試資料一致)', () => {
  it.each([
    ['04595257', true],
    ['10458575', true],
    ['10458574', true],
    ['22099131', true],
    ['10458573', false],
    ['12345678', false],
    ['0459525', false],
    ['0459525A', false],
  ])('%s → %s', (input, want) => {
    expect(validTaxId(input)).toBe(want)
  })
})
