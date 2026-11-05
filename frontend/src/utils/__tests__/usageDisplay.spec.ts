import { afterEach, describe, expect, it, vi } from 'vitest'
import { currencyRate } from '@/utils/currency'
import { formatUsageDuration, formatUsageInteger, formatUsageMoney, formatUsageTokens } from '../usageDisplay'

const { language } = vi.hoisted(() => ({ language: { value: 'en' } }))
vi.mock('@/i18n', () => ({ getLocale: () => language.value }))
afterEach(() => { currencyRate.value = null; language.value = 'en' })

describe('usage display formatters', () => {
  it.each([[0, '0'], [999, '999'], [1000, '1.00K'], [487190000, '487.19M'], [6893947355, '6.89B']])('formats %s tokens as %s', (value, expected) => {
    expect(formatUsageTokens(value)).toBe(expected)
  })
  it.each([[0, '0ms'], [250.4, '250ms'], [999.9, '1000ms'], [1000, '1.00s'], [13206.111, '13.21s']])('formats %s milliseconds as %s', (value, expected) => {
    expect(formatUsageDuration(value)).toBe(expected)
  })
  it.each([null, undefined, NaN, Infinity, -Infinity])('guards missing or invalid %s without coercing to zero', value => {
    expect(formatUsageTokens(value)).toBe('—')
    expect(formatUsageDuration(value)).toBe('—')
    expect(formatUsageInteger(value)).toBe('—')
    expect(formatUsageMoney(value)).toBe('—')
  })
  it.each(['en', 'zh'])('uses grouped integers and a single ledger conversion in %s', locale => {
    language.value = locale
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    expect(formatUsageInteger(38081)).toBe('38,081')
    expect(formatUsageInteger(216293)).toBe('216,293')
    expect(formatUsageMoney(2122.42 / 7)).toBe('¥2,122.42')
    expect(formatUsageMoney(719.97 / 7)).toBe('¥719.97')
    expect(formatUsageMoney(0)).toBe('¥0.00')
  })
  it('falls back to grouped USD when the exchange rate is unavailable or invalid', () => {
    currencyRate.value = null
    expect(formatUsageMoney(2122.42)).toBe('$2,122.42')
    expect(formatUsageMoney(0)).toBe('$0.00')
    currencyRate.value = { cnyPerUsd: NaN, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    expect(formatUsageMoney(2122.42)).toBe('$2,122.42')
  })
})
