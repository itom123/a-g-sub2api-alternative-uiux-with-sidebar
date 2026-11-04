import { getLocale } from '@/i18n'
import { currencyRate } from '@/utils/currency'

type UsageValue = number | null | undefined
const isValue = (value: UsageValue): value is number => typeof value === 'number' && Number.isFinite(value)
const locale = () => getLocale() === 'zh' ? 'zh-CN' : 'en-US'

/** Keep the usage page's two-decimal K/M/B contract. Missing is not zero. */
export function formatUsageTokens(value: UsageValue): string {
  if (!isValue(value)) return '—'
  if (value >= 1e9) return `${(value / 1e9).toFixed(2)}B`
  if (value >= 1e6) return `${(value / 1e6).toFixed(2)}M`
  if (value >= 1e3) return `${(value / 1e3).toFixed(2)}K`
  return new Intl.NumberFormat(locale()).format(value)
}

export function formatUsageDuration(ms: UsageValue): string {
  if (!isValue(ms)) return '—'
  return ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`
}

export function formatUsageInteger(value: UsageValue): string {
  return isValue(value) ? new Intl.NumberFormat(locale(), { maximumFractionDigits: 0 }).format(value) : '—'
}

/** Input is always ledger USD; convert once, or explicitly fall back to USD. */
export function formatUsageMoney(usd: UsageValue): string {
  if (!isValue(usd)) return '—'
  const rate = currencyRate.value?.cnyPerUsd
  const hasRate = typeof rate === 'number' && Number.isFinite(rate) && rate > 0
  const value = hasRate ? usd * rate : usd
  if (!Number.isFinite(value)) return '—'
  return `${hasRate ? '¥' : '$'}${new Intl.NumberFormat(locale(), { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)}`
}
