<template>
  <section class="card p-4" data-testid="account-consistency">
    <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.consistency.title') }}</h3>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.consistency.subtitle') }}</p>
    </div>
    <LoadingSpinner v-if="loading" class="mx-auto" />
    <div v-else-if="summary" class="account-consistency-metrics">
      <div v-for="metric in metrics" :key="metric.key" class="min-w-0 border border-gray-200 p-3 dark:border-dark-700" :data-testid="`consistency-${metric.key}`">
        <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
        <p class="mt-1 break-words text-base font-semibold leading-6 text-gray-900 dark:text-white">{{ metric.value }}</p>
      </div>
    </div>
    <p v-else class="text-sm text-gray-500">{{ t('accountInsights.consistency.noData') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DashboardConsistency } from './types'
const props = defineProps<{ summary: DashboardConsistency | null; loading: boolean }>()
const { t } = useI18n()
const display = (value: number | null | undefined) => value == null || !Number.isFinite(value) ? '—' : Math.round(value).toLocaleString()
const metrics = computed(() => {
  const summary = props.summary
  if (!summary) return []
  const activeRate = summary.activeDays30 == null || !Number.isFinite(summary.activeDays30) ? '—' : `${(summary.activeDays30 / 30 * 100).toFixed(1)}%`
  const values = {
    requests: display(summary.requests),
    activeDays: display(summary.activeDays30),
    activeRate,
    callsPerActiveDay: display(summary.callsPerActiveDay),
    tokensPerCall: display(summary.tokensPerCall),
    recentCalls: display(summary.recentCalls),
    priorCalls: display(summary.priorCalls),
    sevenDayTrend: t(`accountInsights.consistency.${summary.trend7d === 'no_data' ? 'noData' : summary.trend7d}`),
  }
  return Object.entries(values).map(([key, value]) => ({ key, label: t(`accountInsights.consistency.${key}`), value }))
})
</script>
<style scoped>
.account-consistency-metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.account-consistency-metrics > div > p:first-child { min-height: 20px; }
.account-consistency-metrics > div > p:last-child { font-variant-numeric: proportional-nums; overflow-wrap: anywhere; }
@media (min-width: 768px) { .account-consistency-metrics { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
</style>
