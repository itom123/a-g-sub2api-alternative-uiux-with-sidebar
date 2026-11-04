<template>
  <div class="space-y-6" data-testid="account-insights-charts">
    <div class="account-chart-columns" data-testid="account-chart-columns">
      <section ref="comparisonRoot" class="card min-w-0 account-trends" data-testid="account-trends" tabindex="0" :aria-label="t('accountInsights.charts.compareLabel')" aria-describedby="account-trend-help" @keydown="onKeydown">
        <section class="account-trend-panel" data-testid="account-spending-trend">
          <div class="account-trend-heading">
            <h3>{{ t('accountInsights.charts.spendTrend') }}</h3>
            <p :title="t('accountInsights.stats.includesSubscription')">{{ t('accountInsights.charts.actualSpendInRange') }}: <strong>{{ formatMoney(actualSpendInRangeUsd) }}</strong></p>
          </div>
          <div class="account-trend-band" :style="bandStyle"><p>{{ t('accountInsights.charts.compareHint') }}</p></div>
          <div class="account-trend-canvas">
            <LoadingSpinner v-if="loading && !costData" class="mx-auto" />
            <Line v-else-if="costData" :data="costData" :options="costOptions" :plugins="pairPlugins" :class="{ 'opacity-50': loading }" role="img" :aria-label="t('accountInsights.charts.spendTrend')" />
            <p v-else class="account-chart-empty">{{ t('accountInsights.charts.noData') }}</p>
          </div>
        </section>
        <section class="account-trend-panel account-token-panel" data-testid="account-token-trend">
          <div class="account-trend-heading"><h3>{{ t('admin.dashboard.tokenUsageTrend') }}</h3></div>
          <div ref="legendRoot" class="account-trend-band account-trend-legend" :style="bandStyle">
            <button v-for="(series, index) in tokenSeries" :key="series.key" type="button" role="checkbox" :aria-checked="!hiddenSeries.includes(index)" @click="toggleSeries(index)">
              <svg width="24" height="12" aria-hidden="true"><path d="M0 6H24" fill="none" stroke="currentColor" stroke-width="1.5" :stroke-dasharray="comparisonDashes[index]" /></svg>
              <span :class="{ 'line-through': hiddenSeries.includes(index) }">{{ t(`accountInsights.charts.${series.label}`) }}</span>
            </button>
          </div>
          <TokenUsageTrend :trend-data="orderedTrend" :labels="labels" :loading="loading && !trend.length" :embedded="true" :comparison="tokenComparison" />
        </section>
        <div class="account-trend-readout" data-testid="account-trend-readout" aria-live="polite" aria-atomic="true">
          <template v-if="selectedPoint">
            <strong>{{ selectedPoint.date }}</strong>
            <dl><div><dt>{{ t('accountInsights.charts.actual') }}</dt><dd>{{ formatMoney(selectedPoint.actual_cost) }}</dd></div><div v-for="series in tokenSeries" :key="series.key"><dt>{{ t(`accountInsights.charts.${series.label}`) }}</dt><dd>{{ formatNumberLocaleString(selectedPoint[series.key]) }}</dd></div></dl>
          </template>
          <span v-else>{{ t('accountInsights.charts.noSelection') }}</span>
          <p id="account-trend-help">{{ t('accountInsights.charts.compareHelp') }}</p>
        </div>
        <details v-if="trend.length" class="border-t p-4 text-xs" data-testid="account-trend-table">
          <summary class="cursor-pointer">{{ t('accountInsights.charts.viewData') }}</summary>
          <div class="mt-3 overflow-x-auto">
            <table class="w-full text-right tabular-nums">
              <thead><tr class="border-b"><th class="px-2 py-2 text-left">{{ t('accountInsights.charts.date') }}</th><th class="px-2 py-2">{{ t('accountInsights.stats.actualCost') }}</th><th v-for="series in tokenSeries" :key="series.key" class="px-2 py-2">{{ t(`accountInsights.charts.${series.label}`) }}</th></tr></thead>
              <tbody><tr v-for="point in orderedTrend" :key="point.date" class="border-b"><td class="whitespace-nowrap px-2 py-2 text-left">{{ point.date }}</td><td class="whitespace-nowrap px-2 py-2">{{ formatMoney(point.actual_cost) }}</td><td v-for="series in tokenSeries" :key="series.key" class="px-2 py-2">{{ formatNumberLocaleString(point[series.key]) }}</td></tr></tbody>
            </table>
          </div>
        </details>
      </section>
      <ModelDistributionChart class="account-models" :model-stats="models" :loading="loading" :enable-breakdown="false" :enable-ranking-view="false" :show-source-toggle="false" :show-account-cost="false" />
    </div>
    <div class="account-group-distribution"><GroupDistributionChart :group-stats="groups" :loading="loading" :enable-breakdown="false" :show-account-cost="false" /></div>
    <div class="sr-only" aria-live="polite">{{ tableSummary }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useElementSize } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import { comparisonDashes, comparisonMaximum, comparisonScales, comparisonTickIndices, createTrendComparison } from '@/components/charts/trendComparison'
import { currencyRate } from '@/utils/currency'
import { formatNumberLocaleString } from '@/utils/format'
import { formatUsageMoney } from '@/utils/usageDisplay'
import type { GroupStat, ModelStat, TrendDataPoint, UsageStatsResponse } from '@/types'
import type { DashboardRange } from './insights/types'
import { accountGroupContrastPlugin } from './insights/groupContrastPlugin'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler, accountGroupContrastPlugin)
const props = defineProps<{ loading: boolean; range: DashboardRange; trend: TrendDataPoint[]; models: ModelStat[]; groups: GroupStat[]; rangeStats: UsageStatsResponse | null }>()
const { t } = useI18n()
const comparisonRoot = ref<HTMLElement | null>(null)
const legendRoot = ref<HTMLElement | null>(null)
const { width } = useElementSize(comparisonRoot)
const { height: legendHeight } = useElementSize(legendRoot)
const bandStyle = computed(() => ({ minHeight: `${Math.max(32, legendHeight.value)}px` }))
const tokenSeries = [
  { key: 'input_tokens', label: 'input' }, { key: 'output_tokens', label: 'output' },
  { key: 'cache_creation_tokens', label: 'cacheCreation' }, { key: 'cache_read_tokens', label: 'cacheRead' },
] as const
const orderedTrend = computed(() => [...props.trend].sort((a, b) => a.date.localeCompare(b.date)))
const labels = computed(() => orderedTrend.value.map(point => point.date))
const selectedIndex = ref<number | null>(null)
const selectedPoint = computed(() => selectedIndex.value === null ? null : orderedTrend.value[selectedIndex.value] || null)
const hiddenSeries = ref<number[]>([])
function select(index: number | null) { selectedIndex.value = index; pair.select(index) }
const pair = createTrendComparison(select)
const pairPlugins = [pair.plugin]
const layout = computed(() => ({ labels: labels.value, tickIndices: comparisonTickIndices(labels.value.length, width.value - 32), gutter: 80 }))
const tokenComparison = computed(() => ({ layout: { ...layout.value, maximum: comparisonMaximum(orderedTrend.value.flatMap(point => tokenSeries.map(series => Number(point[series.key])))) }, plugins: pairPlugins }))
const actualSpendInRangeUsd = computed(() => {
  const rawTotal = props.rangeStats?.total_actual_cost
  if (rawTotal != null && Number.isFinite(Number(rawTotal))) return Number(rawTotal)
  return orderedTrend.value.reduce((total, point) => total + (Number.isFinite(Number(point.actual_cost)) ? Number(point.actual_cost) : 0), 0)
})
const formatMoney = (value: number | null | undefined) => {
  const usd = value == null ? NaN : Number(value)
  if (!Number.isFinite(usd)) return '—'
  return currencyRate.value ? formatUsageMoney(usd) : `$${usd.toFixed(4)}`
}
const costData = computed(() => !orderedTrend.value.length ? null : {
  labels: labels.value,
  datasets: [{ label: t('accountInsights.charts.actual'), data: orderedTrend.value.map(point => typeof point.actual_cost === 'number' && Number.isFinite(point.actual_cost) ? point.actual_cost : null), borderWidth: 2, pointRadius: 0, pointHoverRadius: 4, fill: false, tension: 0.15 }],
})
const costOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false, animation: false,
  layout: { padding: { top: 8, right: 18, bottom: 0, left: 0 } },
  interaction: { intersect: false, mode: 'index' },
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: context => `${context.dataset.label}: ${formatMoney(Number(context.raw))}` } } },
  scales: comparisonScales({ ...layout.value, maximum: comparisonMaximum(orderedTrend.value.map(point => Number(point.actual_cost))) }, currencyRate.value ? 'CNY' : 'USD', value => {
    const converted = currencyRate.value ? value * currencyRate.value.cnyPerUsd : value
    return converted >= 1000 ? `${(converted / 1000).toFixed(1)}K` : Number(converted.toFixed(2)).toLocaleString()
  }),
}))
function toggleSeries(index: number) {
  hiddenSeries.value = hiddenSeries.value.includes(index) ? hiddenSeries.value.filter(value => value !== index) : [...hiddenSeries.value, index]
  pair.setHidden(hiddenSeries.value)
}
function onKeydown(event: KeyboardEvent) {
  if (event.target !== comparisonRoot.value || !labels.value.length) return
  let next = selectedIndex.value ?? 0
  if (event.key === 'ArrowRight') next = Math.min(labels.value.length - 1, next + 1)
  else if (event.key === 'ArrowLeft') next = Math.max(0, next - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = labels.value.length - 1
  else if (event.key === 'Escape') { event.preventDefault(); select(null); return }
  else return
  event.preventDefault(); select(next)
}
watch(() => props.trend, () => select(null))
const tableSummary = computed(() => `${formatNumberLocaleString(props.models.length)} ${t('accountInsights.charts.model')}, ${formatNumberLocaleString(props.groups.length)} ${t('accountInsights.charts.group')}`)
</script>

<style scoped>
.account-chart-columns { display: grid; grid-template-columns: minmax(0, 1fr); gap: 24px; align-items: stretch; }
.account-trends { display: flex; flex-direction: column; }
.account-trend-panel { min-width: 0; padding: 16px; }
.account-token-panel { border-top: 1px solid var(--ag-rule); }
.account-trend-heading { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 8px; min-height: 24px; font-size: 12px; }
.account-trend-heading h3 { font-size: 14px; font-weight: 600; }
.account-trend-heading p { font-size: 12px; }
.account-trend-band { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; margin: 8px 0; font-size: 12px; }
.account-trend-legend button { display: inline-flex; align-items: center; gap: 6px; min-height: 28px; }
.account-trend-canvas, :deep(.account-token-embedded .ag-token-canvas) { position: relative; height: 240px; min-height: 240px; }
.account-trend-canvas > canvas, :deep(.account-token-embedded .ag-token-canvas > canvas) { position: absolute; inset: 0; }
.account-chart-empty { display: flex; height: 100%; align-items: center; justify-content: center; font-size: 14px; }
.account-trend-readout { border-top: 1px solid var(--ag-rule); padding: 12px 16px; font-size: 12px; }
.account-trend-readout > strong { display: block; margin-bottom: 8px; }
.account-trend-readout dl { display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); gap: 12px; }
.account-trend-readout dd { margin-top: 4px; font-weight: 600; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.account-trend-readout > p { margin-top: 8px; }
.account-trends table th:not(:first-child) { text-align: right; }
@media (min-width: 640px) { .account-trend-canvas, :deep(.account-token-embedded .ag-token-canvas) { height: 260px; min-height: 260px; } }
@media (min-width: 1280px) { .account-chart-columns { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
