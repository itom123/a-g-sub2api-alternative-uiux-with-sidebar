<template>
  <div v-if="!profile" class="card p-4" data-testid="account-insights-stats" role="status">
    {{ t('accountInsights.awaitingData') }}
  </div>
  <div v-else class="space-y-4" data-testid="account-insights-stats">
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <section class="card min-w-0 p-4" data-testid="balance-card">
        <div class="flex items-start justify-between gap-3">
          <div>
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.balance.title') }}</p>
            <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white" data-testid="account-balance">
              <span v-if="hasRate">¥{{ cnyNumber(profile.balance, 2) }}</span>
              <span v-else>${{ formatUsd(profile.balance) }}</span>
            </p>
          </div>
          <span class="rounded-full px-2 py-1 text-xs" :class="profile.balance > 0 ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'">
            <span aria-hidden="true">{{ profile.balance > 0 ? '✓' : '!' }}</span>
            {{ profile.balance > 0 ? t('accountInsights.balance.available') : t('accountInsights.balance.depleted') }}
          </span>
        </div>
        <div v-if="profile.frozen_balance && profile.frozen_balance > 0" class="mt-3 space-y-1 text-xs text-gray-600 dark:text-gray-300">
          <meter class="h-2 w-full" min="0" :max="Math.max(0, profile.balance) + profile.frozen_balance" :value="Math.max(0, profile.balance)" :aria-label="t('accountInsights.balance.available')" />
          <p>{{ t('accountInsights.balance.frozen') }}: <span v-if="hasRate">¥{{ cnyNumber(profile.frozen_balance, 2) }}</span><span v-else>${{ formatUsd(profile.frozen_balance) }}</span></p>
        </div>
        <p v-if="!hasRate" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('accountInsights.warning.currencyHint') }}</p>
      </section>
      <section class="card min-w-0 p-4" data-testid="health-card">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.scope') }}</p>
        <div class="mt-2 flex items-center gap-2"><UserDashboardHealthChip :health="health" /></div>
        <p v-if="!rangeStats" class="mt-3 text-sm text-gray-600 dark:text-gray-300">{{ t('accountInsights.awaitingData') }}</p>
      </section>
    </div>

    <section class="card p-4" data-testid="account-stat-grid">
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-7">
        <Stat :label="t('accountInsights.stats.rangeSpend')" :value="cost(rangeStats?.total_actual_cost)" />
        <Stat :label="t('accountInsights.stats.todaySpend')" :value="cost(dashboardStats.today_actual_cost)" />
        <Stat :label="t('accountInsights.stats.requests')" :value="number(rangeStats?.total_requests ?? dashboardStats.total_requests)" :detail="t('accountInsights.stats.total')" />
        <Stat :label="t('accountInsights.stats.tokens')" :value="number(rangeStats?.total_tokens ?? dashboardStats.total_tokens)" />
        <Stat :label="t('accountInsights.stats.averageResponse')" :value="`${number(rangeStats?.average_duration_ms ?? dashboardStats.average_duration_ms)} ms`" />
        <Stat :label="t('accountInsights.stats.rpm')" :value="number(dashboardStats.rpm)" />
        <Stat :label="t('accountInsights.stats.tpm')" :value="number(dashboardStats.tpm)" />
      </div>
      <div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4" data-testid="token-composition">
        <Stat :label="t('accountInsights.stats.freshInput')" :value="number(rangeStats?.total_input_tokens ?? dashboardStats.total_input_tokens)" />
        <Stat :label="t('accountInsights.stats.cacheCreation')" :value="number(rangeStats?.total_cache_creation_tokens ?? dashboardStats.total_cache_creation_tokens)" />
        <Stat :label="t('accountInsights.stats.cacheRead')" :value="number(rangeStats?.total_cache_read_tokens ?? dashboardStats.total_cache_read_tokens)" />
        <Stat :label="t('accountInsights.stats.output')" :value="number(rangeStats?.total_output_tokens ?? dashboardStats.total_output_tokens)" />
      </div>
      <div class="mt-4 flex flex-wrap gap-4 text-sm text-gray-600 dark:text-gray-300">
        <span>{{ t('accountInsights.stats.activeKeys') }}: {{ number(dashboardStats.active_api_keys) }}</span>
        <span>{{ t('accountInsights.stats.totalKeys') }}: {{ number(dashboardStats.total_api_keys) }}</span>
      </div>
    </section>

    <UserDashboardBalanceWarning :profile="profile" :visible="lowBalance.visible" :threshold-cny="lowBalance.thresholdCny" :dismissed="lowBalance.dismissed" />
    <UserDashboardRolling30d v-if="rolling30" :summary="rolling30" :loading="false" />

    <section v-if="!simpleMode" class="card p-4" data-testid="quota-bars" data-testid-legacy="platform-breakdown">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.platformBreakdown') }}</h3>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('dashboard.platformCount', platformCount) }}</span>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
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
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import type { User, PlatformQuotaItem } from '@/types'
import type { UserDashboardStats as DashboardStats, PlatformDashboardStats } from '@/api/usage'
import type { DashboardHealth, DashboardRolling30d } from './insights/types'
import type { UsageStatsResponse } from '@/types'
import { currencyRate, cnyNumber, formatCny } from '@/utils/currency'
import UserDashboardHealthChip from './core/UserDashboardHealthChip.vue'
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
const Stat = defineComponent({ props: { label: String, value: String, detail: String }, setup(p) { return () => h('div', { class: 'min-w-0' }, [h('p', { class: 'truncate text-xs text-gray-500 dark:text-gray-400' }, p.label), h('p', { class: 'mt-1 truncate text-lg font-semibold tabular-nums text-gray-900 dark:text-white' }, p.value), p.detail ? h('p', { class: 'text-xs text-gray-400' }, p.detail) : null]) } })
const QuotaBar = defineComponent({ props: { quota: { type: Object, required: true }, window: { type: String, required: true } }, setup(p) { return () => { const q = p.quota as PlatformQuotaItem; const w = p.window as typeof quotaWindows[number]; const limit = q[`${w}_limit_usd`]; const usage = q[`${w}_usage_usd`] ?? 0; if (limit == null) return h('p', { class: 'text-xs text-gray-400' }, `${t(`dashboard.platformQuota.${w}`)}: ${t('accountInsights.quota.unlimited')}`); if (limit === 0) return h('p', { class: 'text-xs text-red-600' }, `${t(`dashboard.platformQuota.${w}`)}: ${t('accountInsights.quota.disabled')}`); const pct = Math.min(100, Math.max(0, usage / limit * 100)); return h('div', { class: 'space-y-1' }, [h('div', { class: 'flex justify-between text-xs text-gray-500' }, [h('span', t(`dashboard.platformQuota.${w}`)), h('span', `${pct.toFixed(0)}%`)]), h('div', { class: 'h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600' }, [h('div', { class: `h-full ${pct >= 80 ? 'bg-amber-500' : 'bg-emerald-500'}`, style: { width: `${pct}%` } })]), q[`${w}_window_resets_at`] ? h('p', { class: 'text-[10px] text-gray-400' }, t('dashboard.platformQuota.resetsAt', { time: new Date(q[`${w}_window_resets_at`] as string).toLocaleString() })) : null]) } } })
</script>
