import { shallowRef } from 'vue'
import { buildApiUrl } from '@/api/url'

export interface CurrencyRate {
  cnyPerUsd: number
  rateDate: string
  source: string
  updatedAt: string
  nextRefreshAt: string
}

export const currencyRate = shallowRef<CurrencyRate | null>(null)

export function toCny(usd: number | string | null | undefined): number {
  return currencyRate.value ? Number(usd ?? 0) * currencyRate.value.cnyPerUsd : NaN
}

export function fromCny(cny: number): number {
  if (!currencyRate.value) throw new Error('Currency rate unavailable')
  return cny / currencyRate.value.cnyPerUsd
}

/** Quota settings use whole yuan; the returned ledger amount remains in USD. */
export function roundCnyQuota(usd: number, minimum = 0): number {
  const value = toCny(usd)
  return Number.isFinite(value) ? fromCny(Math.max(minimum, Math.round(Number(value.toPrecision(12))))) : usd
}

export function cnyNumber(usd: number | string | null | undefined, digits = 2): string {
  const value = toCny(usd)
  return Number.isFinite(value) ? value.toFixed(digits) : '-'
}

export function formatCny(usd: number | string | null | undefined, digits = 2): string {
  const value = cnyNumber(usd, digits)
  return value === '-' ? '-' : `¥${value}`
}

export function cnyCost(usd: number | null | undefined): string {
  const value = toCny(usd)
  if (!Number.isFinite(value)) return '-'
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  return value.toFixed(value >= 1 ? 2 : value >= 0.01 ? 3 : 4)
}

export function cnyPreciseNumber(usd: number): string {
  const value = cnyNumber(usd, 8)
  if (value === '-') return value
  const [whole, fraction] = value.split('.')
  const trimmed = fraction.replace(/0+$/, '')
  return `${whole}.${trimmed.padEnd(2, '0')}`
}

export async function loadCurrencyRate(): Promise<void> {
  try {
    const response = await fetch(buildApiUrl('/currency'), { cache: 'no-store', signal: AbortSignal.timeout(5000) })
    if (!response.ok) return
    const rate = await response.json() as CurrencyRate
    if (Number.isFinite(rate.cnyPerUsd) && rate.cnyPerUsd > 0 && rate.cnyPerUsd < 20) currencyRate.value = rate
  } catch { /* Keep the last successful rate; never fabricate a price. */ }
}

export function scheduleCurrencyRateCheck(): void {
  const next = Date.parse(currencyRate.value?.nextRefreshAt || '')
  const delay = Number.isFinite(next) && next > Date.now() ? next - Date.now() + 5000 : 60000
  window.setTimeout(async () => {
    await loadCurrencyRate()
    scheduleCurrencyRateCheck()
  }, Math.min(delay, 2147483647))
}
