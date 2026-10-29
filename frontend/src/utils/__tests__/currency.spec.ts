import { beforeEach, describe, expect, it } from 'vitest'
import { currencyRate, cnyNumber, cnyPreciseNumber, formatCny, fromCny, toCny } from '../currency'
import { formatScaled } from '../pricing'
import { formatPaymentAmount } from '@/components/payment/currency'

beforeEach(() => {
  currencyRate.value = { cnyPerUsd: 7.123456, source: 'ECB', rateDate: '2026-10-09', updatedAt: '2026-10-10T19:00:00Z', nextRefreshAt: '2026-10-17T19:00:00Z' }
})

describe('CNY presentation over the unchanged USD ledger', () => {
  it('converts before rounding and keeps input round trips exact', () => {
    expect(formatCny(10)).toBe('¥71.23')
    expect(cnyNumber(0.00001234, 8)).toBe('0.00008790')
    expect(fromCny(toCny(123.456))).toBeCloseTo(123.456, 12)
    expect(fromCny(165)).toBeCloseTo(23.163, 2)
  })
  it('preserves zero, refunds and missing-rate safety', () => {
    expect(formatCny(0)).toBe('¥0.00')
    expect(formatCny(-1)).toBe('¥-7.12')
    currencyRate.value = null
    expect(formatCny(10)).toBe('-')
    expect(() => fromCny(10)).toThrow('unavailable')
  })
  it('converts model prices once and never multiplies native CNY settlement cash', () => {
    currencyRate.value = { ...currencyRate.value!, cnyPerUsd: 7 }
    expect(formatScaled(10e-6 * 1.2, 1e6, 2)).toBe('¥84.00')
    expect(formatPaymentAmount(165, 'CNY', 'zh-CN')).toBe('¥165.00')
    expect(formatPaymentAmount(10, 'USD')).toBe('¥70.00')
    expect(cnyPreciseNumber(12.34567891)).toBe('86.41975237')
  })
})
