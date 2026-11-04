<template>
  <div class="space-y-4" data-testid="account-insights-stats">
    <div class="account-overview" data-testid="account-overview">
      <section class="card min-w-0 p-4 account-overview-balance" data-testid="balance-card">
        <p class="account-overview-label text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.balance.title') }}</p>
        <p class="account-overview-value text-2xl font-semibold text-gray-900 dark:text-white" data-testid="account-balance">
          {{ profile ? formatUsageMoney(profile.balance) : '—' }}
        </p>
        <div class="account-balance-detail">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.stats.todaySpend') }}</p>
          <p class="mt-1 text-lg font-semibold tabular-nums">{{ formatUsageMoney(dashboardStats?.today_actual_cost) }}</p>
        </div>
        <p v-if="!hasRate" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('accountInsights.warning.currencyHint') }}</p>
      </section>
      <section class="card min-w-0 p-4" data-testid="active-keys-card">
        <p class="account-overview-label text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.stats.activeKeys') }}</p>
        <p class="account-overview-value text-2xl font-semibold text-gray-900 dark:text-white">{{ number(dashboardStats?.active_api_keys) }}</p>
      </section>
      <section class="card min-w-0 p-4" data-testid="total-keys-card">
        <p class="account-overview-label text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.stats.totalKeys') }}</p>
        <p class="account-overview-value text-2xl font-semibold text-gray-900 dark:text-white">{{ number(dashboardStats?.total_api_keys) }}</p>
      </section>
      <section class="card min-w-0 p-4 account-overview-control" data-testid="date-range-card"><slot name="date-control" /></section>
      <section class="card min-w-0 p-4 account-overview-control" data-testid="refresh-settings-card"><slot name="refresh-control" /></section>
    </div>

    <template v-if="profile">
    <slot name="after-overview" />

    <section class="space-y-4" data-testid="account-stat-grid">
      <DashboardStatGrid data-testid="primary-metrics">
        <Stat v-for="metric in primaryMetrics" :key="metric.key" v-bind="{ label: metric.label, value: metric.value, exact: metric.exact, support: metric.support }" :support-value="'supportValue' in metric && typeof metric.supportValue === 'string' ? metric.supportValue : undefined" :data-metric="metric.key" />
      </DashboardStatGrid>

      <section aria-labelledby="account-current-rates" data-testid="current-rates">
        <h3 id="account-current-rates" class="account-group-label">{{ t('accountInsights.stats.currentRates') }}</h3>
        <div class="ag-stat-grid grid grid-cols-2 gap-4">
          <Stat v-for="metric in rateMetrics" :key="metric.key" v-bind="{ label: metric.label, value: metric.value, exact: metric.exact, support: metric.support }" :data-metric="metric.key" />
        </div>
      </section>

      <section aria-labelledby="account-token-composition" data-testid="token-composition">
        <h3 id="account-token-composition" class="account-group-label">{{ t('accountInsights.stats.tokenComposition') }}</h3>
        <DashboardStatGrid>
          <Stat v-for="metric in tokenMetrics" :key="metric.key" v-bind="{ label: metric.label, value: metric.value, exact: metric.exact, support: metric.support }" :data-metric="metric.key" />
        </DashboardStatGrid>
      </section>

      <details class="card account-exact p-4" data-testid="exact-values">
        <summary class="cursor-pointer text-sm font-medium">{{ t('accountInsights.stats.exactValues') }}</summary>
        <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.stats.ledgerCurrency') }}</p>
        <dl class="account-exact-grid mt-3">
          <div v-for="metric in exactMetrics" :key="metric.key" class="account-exact-row" :data-exact-metric="metric.key">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</dt>
            <dd class="text-sm font-medium">{{ metric.exact }}</dd>
          </div>
        </dl>
      </details>
      <p class="sr-only">{{ t('accountInsights.stats.includesSubscription') }}</p>
    </section>

    <UserDashboardBalanceWarning :profile="profile" :visible="lowBalance.visible" :threshold-cny="lowBalance.thresholdCny" :dismissed="lowBalance.dismissed" />
    <UserDashboardRolling30d v-if="rolling30" :summary="rolling30" :loading="false" />

    <section v-if="!simpleMode" class="card p-4" data-testid="quota-bars" data-testid-legacy="platform-breakdown">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.platformBreakdown') }}</h3>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('dashboard.platformCount', platformCount) }}</span>
      </div>
      <div class="account-platforms">
        <div v-for="item in platformCards" :key="item.platform" data-testid="platform-card" :data-platform="item.platform" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.isOther ? t('dashboard.platformOther') : platformLabel(item.platform) }}</span>
            <span class="font-mono text-sm text-purple-600 dark:text-purple-400">{{ cost(item.total_actual_cost) }}</span>
          </div>
          <div class="mt-2 space-y-1 text-xs">
            <div class="flex items-center justify-between"><span class="text-gray-500 dark:text-gray-400">{{ t('dashboard.todayCost') }}</span><span>{{ cost(item.today_actual_cost) }}</span></div>
            <div class="flex items-center justify-between"><span class="text-gray-500 dark:text-gray-400">{{ t('dashboard.requests') }}</span><span>{{ item.total_requests > 0 ? number(item.total_requests) : '-' }}</span></div>
            <div class="flex items-center justify-between"><span class="text-gray-500 dark:text-gray-400">{{ t('dashboard.tokens') }}</span><span>{{ item.total_tokens > 0 ? number(item.total_tokens) : '-' }}</span></div>
          </div>
          <div v-if="item.quota && !item.isOther && hasAnyLimit(item.quota)" class="mt-3 space-y-2 border-t border-gray-200 pt-2 dark:border-dark-700">
            <p class="text-[10px] uppercase tracking-wide text-gray-400">{{ t('dashboard.platformQuota.title') }}</p>
            <QuotaBar v-for="window in quotaWindows" :key="window" :quota="item.quota" :window="window" />
          </div>
        </div>
      </div>
    </section>
    </template>
    <p v-else role="status" class="text-sm">{{ t('accountInsights.awaitingData') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLocale } from '@/i18n'
import type { User, PlatformQuotaItem } from '@/types'
import type { UserDashboardStats as DashboardStats, PlatformDashboardStats } from '@/api/usage'
import type { DashboardHealth, DashboardRolling30d } from './insights/types'
import type { UsageStatsResponse } from '@/types'
import { currencyRate, formatCny } from '@/utils/currency'
import { formatUsageTokens, formatUsageDuration, formatUsageInteger, formatUsageMoney } from '@/utils/usageDisplay'
import DashboardStatGrid from '@/components/common/DashboardStatGrid.vue'
import UserDashboardBalanceWarning from './core/UserDashboardBalanceWarning.vue'
import UserDashboardRolling30d from './core/UserDashboardRolling30d.vue'

interface PlatformCard extends PlatformDashboardStats { isOther?: boolean; quota?: PlatformQuotaItem }
const props = withDefaults(defineProps<{
  profile?: User | null
  dashboardStats?: DashboardStats
  rangeStats?: UsageStatsResponse | null
  rolling30?: DashboardRolling30d | null
  quotas?: PlatformQuotaItem[]
  health?: DashboardHealth
  lowBalance?: { visible: boolean; thresholdCny: number; dismissed: boolean }
  isSimple?: boolean
}>(), {
  profile: null,
  dashboardStats: undefined,
  rangeStats: null,
  rolling30: null,
  quotas: () => [],
  health: 'awaiting_data',
  lowBalance: () => ({ visible: false, thresholdCny: 210, dismissed: false }),
  isSimple: false,
})
const { t } = useI18n()
const hasRate = computed(() => Boolean(currencyRate.value))
const simpleMode = computed(() => props.isSimple)
const quotaWindows = ['daily', 'weekly', 'monthly'] as const
const platformOrder = ['openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'typesafe']
function number(value: number | null | undefined): string { return value == null || !Number.isFinite(Number(value)) ? '—' : Number(value).toLocaleString() }
function formatUsd(value: number | null | undefined): string { return value == null || !Number.isFinite(Number(value)) ? '—' : Number(value).toFixed(4) }
function cost(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(Number(value))) return '—'
  const formatted = formatCny(value, 2)
  return formatted === '-' ? `$${formatUsd(value)}` : formatted
}
function exactNumber(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value)
    ? new Intl.NumberFormat(getLocale() === 'zh' ? 'zh-CN' : 'en-US', { maximumFractionDigits: 20 }).format(value)
    : '—'
}
function exactWithUnit(value: number | null | undefined, unit: string): string {
  const formatted = exactNumber(value)
  return formatted === '—' ? formatted : `${formatted} ${unit}`
}
const primaryMetrics = computed(() => [
  { key: 'rangeSpend', label: t('accountInsights.stats.rangeSpend'), value: formatUsageMoney(props.rangeStats?.total_actual_cost), exact: exactWithUnit(props.rangeStats?.total_actual_cost, 'USD'), support: t('accountInsights.stats.selectedRange') },
  { key: 'requests', label: t('accountInsights.stats.requests'), value: formatUsageInteger(props.rangeStats?.total_requests), exact: exactNumber(props.rangeStats?.total_requests), support: t('accountInsights.stats.selectedRange') },
  { key: 'tokens', label: t('accountInsights.stats.totalTokens'), value: formatUsageTokens(props.rangeStats?.total_tokens), exact: exactNumber(props.rangeStats?.total_tokens), support: t('accountInsights.stats.selectedRange') },
  { key: 'averageResponse', label: t('accountInsights.stats.averageResponse'), value: formatUsageDuration(props.rangeStats?.average_duration_ms), exact: exactWithUnit(props.rangeStats?.average_duration_ms, 'ms'), support: t('accountInsights.stats.selectedRange') },
])
const rateMetrics = computed(() => [
  { key: 'rpm', label: t('accountInsights.stats.rpm'), value: formatUsageInteger(props.dashboardStats?.rpm), exact: exactNumber(props.dashboardStats?.rpm), support: t('accountInsights.stats.recentAverage') },
  { key: 'tpm', label: t('accountInsights.stats.tpm'), value: formatUsageInteger(props.dashboardStats?.tpm), exact: exactNumber(props.dashboardStats?.tpm), support: t('accountInsights.stats.recentAverage') },
])
const tokenMetrics = computed(() => [
  { key: 'freshInput', raw: props.rangeStats?.total_input_tokens },
  { key: 'cacheCreation', raw: props.rangeStats?.total_cache_creation_tokens },
  { key: 'cacheRead', raw: props.rangeStats?.total_cache_read_tokens },
  { key: 'output', raw: props.rangeStats?.total_output_tokens },
].map(({ key, raw }) => ({ key, label: t(`accountInsights.stats.${key}`), value: formatUsageTokens(raw), exact: exactNumber(raw), support: t('accountInsights.stats.selectedRange') })))
const exactMetrics = computed(() => [
  ...primaryMetrics.value.slice(0, 1),
  { key: 'todaySpend', label: t('accountInsights.stats.todaySpend'), exact: exactWithUnit(props.dashboardStats?.today_actual_cost, 'USD') },
  ...primaryMetrics.value.slice(1),
  ...rateMetrics.value,
  ...tokenMetrics.value,
])
function platformLabel(platform: string): string { const labels: Record<string, string> = { anthropic: 'Claude', openai: 'OpenAI', gemini: 'Gemini', antigravity: 'Antigravity', grok: 'Grok', typesafe: 'TypeSafe / Jev' }; return labels[platform] || `${platform.slice(0, 1).toUpperCase()}${platform.slice(1)}` }
function hasAnyLimit(quota?: PlatformQuotaItem): boolean { return Boolean(quota && quotaWindows.some((window) => quota[`${window}_limit_usd`] !== null && quota[`${window}_limit_usd`] !== undefined)) }
  const platformCards = computed<PlatformCard[]>(() => {
  const stats = props.dashboardStats?.by_platform ?? []
  const quotas = props.quotas ?? []
  const byPlatform = new Map<string, PlatformQuotaItem>(quotas.map((quota) => [String(quota.platform), quota]))
  const merged = new Map<string, PlatformCard>()
  for (const raw of stats) {
    const item = raw as PlatformDashboardStats & { requests?: number; tokens?: number; actual_cost?: number; today_cost?: number }
    merged.set(String(item.platform), {
      ...item,
      platform: String(item.platform),
      total_requests: Number(item.total_requests ?? item.requests ?? 0),
      total_tokens: Number(item.total_tokens ?? item.tokens ?? 0),
      total_actual_cost: Number(item.total_actual_cost ?? item.actual_cost ?? 0),
      today_requests: Number(item.today_requests ?? 0),
      today_tokens: Number(item.today_tokens ?? 0),
      today_actual_cost: Number(item.today_actual_cost ?? item.today_cost ?? 0),
      quota: byPlatform.get(String(item.platform)),
    })
  }
  for (const quota of quotas) if (!merged.has(String(quota.platform))) merged.set(String(quota.platform), { platform: String(quota.platform), total_requests: 0, total_tokens: 0, total_actual_cost: 0, today_requests: 0, today_tokens: 0, today_actual_cost: 0, quota })
  const ordered = [...merged.values()].sort((a, b) => { const ai = platformOrder.indexOf(a.platform); const bi = platformOrder.indexOf(b.platform); return (ai < 0 ? 100 : ai) - (bi < 0 ? 100 : bi) || a.platform.localeCompare(b.platform) })
  const total = Number(props.dashboardStats?.total_actual_cost ?? 0)
  const covered = ordered.reduce((sum, item) => sum + Number(item.total_actual_cost || 0), 0)
  if (Number.isFinite(total) && stats.length > 0 && total > covered + 1e-9) ordered.push({ platform: '__other__', total_requests: 0, total_tokens: 0, total_actual_cost: total - covered, today_requests: 0, today_tokens: 0, today_actual_cost: 0, isOther: true })
  return ordered.filter((item) => item.isOther || item.total_requests > 0 || item.total_tokens > 0 || item.total_actual_cost > 0 || hasAnyLimit(item.quota))
})
const platformCount = computed(() => platformCards.value.filter((item) => !item.isOther).length)
const Stat = defineComponent({
  props: { label: String, value: String, exact: String, support: String, supportValue: String },
  setup(p) {
    return () => h('dl', { class: 'card account-metric min-w-0 p-4' }, [
      h('dt', { class: 'account-metric-label text-xs font-medium text-gray-500 dark:text-gray-400' }, p.label),
      h('dd', { class: 'account-metric-value font-semibold', title: `${p.label}: ${p.exact}`, 'aria-label': `${p.label}: ${p.exact}` }, p.value),
      h('dd', { class: 'account-metric-support text-xs text-gray-500 dark:text-gray-400' }, [h('span', p.support), p.supportValue == null ? null : h('span', { class: 'whitespace-nowrap' }, p.supportValue)]),
    ])
  },
})
const QuotaBar = defineComponent({ props: { quota: { type: Object, required: true }, window: { type: String, required: true } }, setup(p) { return () => { const q = p.quota as PlatformQuotaItem; const w = p.window as typeof quotaWindows[number]; const limit = q[`${w}_limit_usd`]; const usage = q[`${w}_usage_usd`] ?? 0; if (limit == null) return h('p', { class: 'text-xs text-gray-400' }, `${t(`dashboard.platformQuota.${w}`)}: ${t('accountInsights.quota.unlimited')}`); if (limit === 0) return h('p', { class: 'text-xs text-red-600' }, `${t(`dashboard.platformQuota.${w}`)}: ${t('accountInsights.quota.disabled')}`); const pct = Math.min(100, Math.max(0, usage / limit * 100)); return h('div', { class: 'space-y-1' }, [h('div', { class: 'flex justify-between text-xs text-gray-500' }, [h('span', t(`dashboard.platformQuota.${w}`)), h('span', `${pct.toFixed(0)}%`)]), h('div', { class: 'h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600' }, [h('div', { class: `h-full ${pct >= 80 ? 'bg-amber-500' : 'bg-emerald-500'}`, style: { width: `${pct}%` } })]), q[`${w}_window_resets_at`] ? h('p', { class: 'text-[10px] text-gray-400' }, t('dashboard.platformQuota.resetsAt', { time: new Date(q[`${w}_window_resets_at`] as string).toLocaleString() })) : null]) } } })
</script>

<style scoped>
.account-overview { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.account-overview-balance { grid-column: 1 / -1; display: flex; flex-direction: column; }
.account-balance-detail { margin-top: auto; padding-top: 24px; }
.account-overview-control { display: flex; flex-direction: column; justify-content: flex-start; }
.account-overview-label { min-height: 20px; line-height: 20px; }
.account-platforms { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 15rem), 1fr)); gap: 12px; }
.account-overview-value { margin-top: 8px; line-height: 32px; font-variant-numeric: proportional-nums; overflow-wrap: anywhere; }
.account-group-label { margin-bottom: 8px; color: var(--ag-ink); font-size: 12px; font-weight: 500; }
:deep(.account-metric) { display: flex; flex-direction: column; color: var(--ag-ink); }
:deep(.account-metric-label) { min-height: 20px; line-height: 20px; }
:deep(.account-metric-value) { margin-top: 4px; font-size: clamp(1rem, 1.8vw, 1.5rem); line-height: 32px; font-variant-numeric: tabular-nums; white-space: nowrap; }
:deep(.account-metric-support) { display: flex; flex-wrap: wrap; column-gap: 6px; min-height: 16px; margin-top: 4px; line-height: 16px; }
.account-exact { color: var(--ag-ink); }
.account-exact > summary:focus-visible { outline: 2px solid var(--ag-ink); outline-offset: 4px; }
.account-exact-grid { display: grid; gap: 0 24px; }
.account-exact-row { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 4px 12px; padding: 10px 0; border-top: 1px solid var(--ag-rule); }
.account-exact-row dd { white-space: nowrap; font-variant-numeric: tabular-nums; }
@media (min-width: 768px) {
  .account-overview { grid-template-columns: minmax(0, 2fr) repeat(2, minmax(0, 1fr)); }
  .account-overview-balance { grid-column: 1; grid-row: 1 / 3; }
  .account-overview { grid-auto-rows: minmax(108px, 1fr); }
  .account-exact-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
