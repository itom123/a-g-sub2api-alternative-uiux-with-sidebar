<template>
  <div class="space-y-6">
    <!-- Date Range Filter -->
    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-4">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('dashboard.timeRange') }}:</span>
          <DateRangePicker :start-date="startDate" :end-date="endDate" @update:startDate="$emit('update:startDate', $event)" @update:endDate="$emit('update:endDate', $event)" @change="$emit('dateRangeChange', $event)" />
        </div>
        <div class="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('dashboard.granularity') }}:</span>
          <div class="w-28">
            <Select :model-value="granularity" :options="[{value:'day', label:t('dashboard.day')}, {value:'hour', label:t('dashboard.hour')}]" @update:model-value="$emit('update:granularity', $event)" @change="$emit('granularityChange')" />
          </div>
        </div>
      </div>
    </div>

    <section class="card min-w-0 p-4" data-testid="account-spending-trend">
      <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.costTrend') }}</h3>
        <p class="text-sm text-gray-600 dark:text-gray-300">
          {{ t('dashboard.periodSpent') }}:
          <strong class="tabular-nums text-gray-900 dark:text-white">¥{{ formatCost(models.reduce((sum, model) => sum + model.actual_cost, 0)) }}</strong>
        </p>
      </div>
      <div class="relative h-52 min-w-0">
        <LoadingSpinner v-if="loading" class="mx-auto" />
        <Line v-else-if="costData" :data="costData" :options="costOptions" role="img" :aria-label="t('dashboard.costTrend')" />
        <p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('dashboard.noDataAvailable') }}</p>
      </div>
    </section>

    <!-- Charts Grid -->
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <!-- Model Distribution Chart -->
      <div class="ag-user-model-distribution card relative min-w-0 overflow-hidden p-4">
        <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/50 backdrop-blur-sm dark:bg-dark-800/50">
          <LoadingSpinner size="md" />
        </div>
        <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.modelDistribution') }}</h3>
        <div class="ag-user-distribution-layout flex flex-col items-center gap-4">
          <div class="relative h-48 w-full max-w-48 shrink-0">
            <Doughnut v-if="modelData" :data="modelData" :options="doughnutOptions" />
            <div v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('dashboard.noDataAvailable') }}</div>
          </div>
          <div class="w-full min-w-0 flex-1 overflow-x-auto sm:max-h-64 sm:overflow-auto">
            <table class="ag-user-distribution-table w-full text-xs">
              <thead>
                <tr class="text-gray-500 dark:text-gray-400">
                  <th class="pb-2 text-left">{{ t('dashboard.model') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.requests') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.tokens') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.actual') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.standard') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in models" :key="model.model" class="border-t border-gray-100 dark:border-dark-700">
                  <td class="max-w-[100px] truncate py-1.5 font-medium text-gray-900 dark:text-white" :title="model.model">{{ model.model }}</td>
                  <td :data-label="t('dashboard.requests')" class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ formatNumber(model.requests) }}</td>
                  <td :data-label="t('dashboard.tokens')" class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ formatTokens(model.total_tokens) }}</td>
                  <td :data-label="t('dashboard.actual')" class="py-1.5 text-right text-green-600 dark:text-green-400">¥{{ formatCost(model.actual_cost) }}</td>
                  <td :data-label="t('dashboard.standard')" class="py-1.5 text-right text-gray-400 dark:text-gray-500">¥{{ formatCost(model.cost) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Token Usage Trend Chart -->
      <TokenUsageTrend :trend-data="trend" :loading="loading" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import { Doughnut, Line } from 'vue-chartjs'
import { toCny, currencyRate } from '@/utils/currency'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler, type ChartOptions } from 'chart.js'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{ loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange'])
const { t } = useI18n()

const costData = computed(() => !props.trend.length || !currencyRate.value ? null : {
  labels: props.trend.map(point => point.date),
  datasets: [{
    label: t('dashboard.actual'),
    data: props.trend.map(point => toCny(point.actual_cost)),
    borderColor: '#000000',
    borderWidth: 1.5,
    pointRadius: 0,
    fill: false,
  }],
})
const costOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  plugins: {
    legend: { display: false },
    tooltip: { callbacks: { label: context => '¥' + Number(context.parsed.y).toFixed(4) } },
  },
  scales: {
    y: { beginAtZero: true, ticks: { callback: value => '¥' + value } },
    x: { ticks: { maxTicksLimit: 8, maxRotation: 0 } },
  },
}

const modelData = computed(() => !props.models?.length ? null : {
  labels: props.models.map((m: ModelStat) => m.model),
  datasets: [{
    data: props.models.map((m: ModelStat) => m.total_tokens),
    backgroundColor: props.models.map((_, i) => i % 2 ? '#ffffff' : '#000000'),
    borderColor: '#000000', borderWidth: 1
  }]
})

const doughnutOptions = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.label}: ${formatTokens(context.parsed)} tokens`
      }
    }
  }
}
</script>
