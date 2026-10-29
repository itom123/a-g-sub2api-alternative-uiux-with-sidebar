<template>
  <div class="card p-4">
    <h3 class="mb-4 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm font-semibold text-gray-900 dark:text-white">
      {{ t('admin.dashboard.tokenUsageTrend') }}
      <span class="text-xs font-normal text-gray-500 dark:text-gray-400">
        {{ t('admin.dashboard.tokenUsageTrendHint') }}
      </span>
    </h3>
    <div v-if="loading" class="ag-token-canvas flex items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="trendData.length > 0 && chartData" class="ag-token-canvas">
      <Line :data="chartData" :options="lineOptions" />
    </div>
    <div
      v-else
      class="ag-token-canvas flex items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { cnyNumber } from '@/utils/currency'

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
  type Scale
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { TrendDataPoint } from '@/types'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)

const { t } = useI18n()

const props = defineProps<{
  trendData: TrendDataPoint[]
  labels?: string[]
  loading?: boolean
}>()

const chartColors = computed(() => ({
  text: '#000000',
  grid: '#000000',
  input: '#000000',
  output: '#000000',
  cacheCreation: '#000000',
  cacheRead: '#000000'
}))

const chartData = computed(() => {
  if (!props.trendData?.length) return null

  const labels = props.labels?.length ? props.labels : props.trendData.map((d) => d.date)

  return {
    labels,
    datasets: [
      {
        label: 'Input',
        data: props.trendData.map((d) => d.input_tokens),
        yAxisID: 'y',
        borderColor: chartColors.value.input,
        backgroundColor: '#ffffff',
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 3,
        spanGaps: true
      },
      {
        label: 'Output',
        borderDash: [8, 4],
        data: props.trendData.map((d) => d.output_tokens),
        yAxisID: 'y',
        borderColor: chartColors.value.output,
        backgroundColor: '#ffffff',
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 3,
        spanGaps: true
      },
      {
        label: 'Cache Creation',
        borderDash: [2, 4],
        data: props.trendData.map((d) => d.cache_creation_tokens),
        yAxisID: 'y',
        borderColor: chartColors.value.cacheCreation,
        backgroundColor: '#ffffff',
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 3,
        spanGaps: true
      },
      {
        label: 'Cache Read',
        borderDash: [12, 4, 2, 4],
        data: props.trendData.map((d) => d.cache_read_tokens),
        yAxisID: 'y',
        borderColor: chartColors.value.cacheRead,
        backgroundColor: '#ffffff',
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 3,
        spanGaps: true
      }
    ]
  }
})

const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: false as const,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'line',
        pointStyleWidth: 64,
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatTokens(context.raw)}`
        },
        footer: (tooltipItems: any) => {
          const dataIndex = tooltipItems[0]?.dataIndex
          if (dataIndex !== undefined && props.trendData[dataIndex]) {
            const data = props.trendData[dataIndex]
            const promptTokens = data.input_tokens + data.cache_read_tokens + data.cache_creation_tokens
            const hitRate = promptTokens > 0 ? data.cache_read_tokens / promptTokens * 100 : 0
            return `Cache Hit Rate: ${hitRate.toFixed(1)}%\nActual: ¥${formatCost(data.actual_cost)} | Standard: ¥${formatCost(data.cost)}`
          }
          return ''
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        maxRotation: 0,
        maxTicksLimit: 8,
        callback(this: Scale, value: string | number) {
          return this.getLabelForValue(Number(value)).replace(/^\d{4}-/, '')
        },
        font: {
          size: 10
        }
      }
    },
    y: {
      position: 'left' as const,
      beginAtZero: true,
      title: {
        display: true,
        text: t('admin.dashboard.tokens'),
        color: chartColors.value.text,
        font: {
          size: 10,
          weight: 'bold' as const
        }
      },
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    }
  }
}))

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  return cnyNumber(value, 4)
}
</script>
