<template>
  <div class="space-y-6" data-testid="account-insights-charts">
    <section class="card min-w-0 p-4" data-testid="account-spending-trend">
      <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.charts.spendTrend') }}</h3>
        <p class="text-sm text-gray-600 dark:text-gray-300">
          {{ t('accountInsights.charts.actualSpendInRange') }}:
          <strong class="tabular-nums text-gray-900 dark:text-white">{{ formatMoney(actualSpendInRangeUsd) }}</strong>
          <span v-if="!hasCurrencyRate" class="ml-2 text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.charts.cnyUnavailable') }}</span>
        </p>
      </div>
      <div class="relative h-52 min-w-0">
        <LoadingSpinner v-if="loading" class="mx-auto" />
        <Line v-else-if="costData" :data="costData" :options="costOptions" role="img" :aria-label="t('accountInsights.charts.spendTrend')" />
        <p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('accountInsights.charts.noData') }}</p>
      </div>
    </section>

    <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-2">
      <TokenUsageTrend :trend-data="trend" :loading="loading" />
      <ModelDistributionChart
        :model-stats="models"
        :loading="loading"
        :enable-breakdown="false"
        :enable-ranking-view="false"
        :show-source-toggle="false"
        :show-account-cost="false"
      />
      <div class="account-group-distribution xl:col-span-2">
        <GroupDistributionChart
        :group-stats="groups"
        :loading="loading"
        :enable-breakdown="false"
        :show-account-cost="false"
        />
      </div>
    </div>

    <div class="sr-only" aria-live="polite">
      {{ tableSummary }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler } from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import { currencyRate, cnyNumber, toCny } from '@/utils/currency'
import { formatNumberLocaleString } from '@/utils/format'
import type { GroupStat, ModelStat, TrendDataPoint, UsageStatsResponse } from '@/types'
import type { DashboardRange } from './insights/types'
import { accountGroupContrastPlugin } from './insights/groupContrastPlugin'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler, accountGroupContrastPlugin)

const props = defineProps<{
  loading: boolean
  range: DashboardRange
  trend: TrendDataPoint[]
  models: ModelStat[]
  groups: GroupStat[]
  rangeStats: UsageStatsResponse | null
}>()
const { t } = useI18n()
const hasCurrencyRate = computed(() => Boolean(currencyRate.value))
const actualSpendInRangeUsd = computed(() => {
  const statsTotal = Number(props.rangeStats?.total_actual_cost)
  if (Number.isFinite(statsTotal)) return statsTotal
  return props.trend.reduce((total, point) => total + (Number.isFinite(Number(point.actual_cost)) ? Number(point.actual_cost) : 0), 0)
})
const formatMoney = (value: number | null | undefined) => {
  const usd = Number(value)
  if (!Number.isFinite(usd)) return '—'
  if (!hasCurrencyRate.value) return `$${usd.toFixed(4)}`
  const number = cnyNumber(usd)
  return number === '-' ? '—' : `¥${number}`
}
const costData = computed(() => {
  if (!props.trend?.length) return null
  return { labels: props.trend.map((point) => point.date), datasets: [{ label: t('accountInsights.charts.actual'), data: props.trend.map((point) => hasCurrencyRate.value ? toCny(Number(point.actual_cost)) : Number(point.actual_cost)), borderColor: '#111827', backgroundColor: 'rgba(17,24,39,.08)', borderWidth: 2, pointRadius: 0, pointHoverRadius: 4, fill: true, tension: 0.25 }] }
})
const costOptions = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false as const,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: { legend: { display: true }, tooltip: { callbacks: { label: (context: any) => `${context.dataset.label}: ${formatMoney(Number(context.raw))}` } } },
  scales: { y: { beginAtZero: true, title: { display: true, text: hasCurrencyRate.value ? 'CNY' : 'USD' } }, x: { ticks: { maxTicksLimit: 8, maxRotation: 0 } } },
}
const tableSummary = computed(() => `${formatNumberLocaleString(props.models.length)} ${t('accountInsights.charts.model')}, ${formatNumberLocaleString(props.groups.length)} ${t('accountInsights.charts.group')}`)
</script>
