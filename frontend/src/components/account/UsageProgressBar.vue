<template>
  <div :class="{ 'ag-usage-window': detailed }">
    <template v-if="detailed">
      <div class="ag-usage-window-heading">
        <strong>{{ fullLabel }}</strong>
        <span>{{ t(remainingCapacity ? 'admin.accounts.usageWindow.remaining' : 'admin.accounts.usageWindow.used') }} {{ displayPercent }}</span>
        <HelpTooltip :content="windowExplanation" width-class="w-80" />
      </div>
      <div
        class="ag-usage-meter"
        role="progressbar"
        :aria-label="`${fullLabel} · ${t(remainingCapacity ? 'admin.accounts.usageWindow.remaining' : 'admin.accounts.usageWindow.used')}`"
        :aria-valuenow="Math.min(Math.max(utilization, 0), 100)"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuetext="displayPercent"
      >
        <span :style="{ width: barWidth }"></span>
      </div>
      <div class="ag-usage-reset-time">
        <span>{{ t('admin.accounts.usageWindow.automaticRecovery') }}</span>
        <strong>{{ shouldShowResetTime ? formatResetTime : t('admin.accounts.usageWindow.notReported') }}</strong>
        <HelpTooltip :content="resetExplanation" width-class="w-80" />
      </div>
      <dl v-if="windowStats && (windowStats.requests > 0 || windowStats.tokens > 0)" class="ag-usage-stats">
        <div><dt>{{ t('admin.accounts.usageWindow.requests') }}</dt><dd>{{ formatRequests }}</dd></div>
        <div><dt>Token</dt><dd>{{ formatTokens }}</dd></div>
        <div><dt>{{ t('usage.accountBilled') }}</dt><dd>¥{{ formatAccountCost }}</dd></div>
        <div v-if="windowStats.user_cost != null"><dt>{{ t('usage.userBilled') }}</dt><dd>¥{{ formatUserCost }}</dd></div>
        <div v-if="estimatedTotalCost != null" data-test="estimated-total-cost">
          <dt>{{ t('admin.accounts.usageWindow.estimatedCostLabel') }} <HelpTooltip :content="t('admin.accounts.usageWindow.estimatedTotalCostTooltip')" width-class="w-80" /></dt>
          <dd>¥{{ cnyNumber(estimatedTotalCost, 2) }}</dd>
        </div>
      </dl>
    </template>
    <template v-else>
    <!-- Window stats row (above progress bar) -->
    <div
      v-if="windowStats && (windowStats.requests > 0 || windowStats.tokens > 0)"
      class="mb-0.5 flex items-center"
    >
      <div class="flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-gray-400">
        <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">
          {{ formatRequests }} req
        </span>
        <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">
          {{ formatTokens }}
        </span>
        <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800" :title="t('usage.accountBilled')">
          A ¥{{ formatAccountCost }}
        </span>
        <span
          v-if="windowStats?.user_cost != null"
          class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800"
          :title="t('usage.userBilled')"
        >
          U ¥{{ formatUserCost }}
        </span>
        <span
          v-if="estimatedTotalCost != null"
          data-test="estimated-total-cost"
          class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800"
          :title="t('admin.accounts.usageWindow.estimatedTotalCostTooltip')"
        >
          {{ t('admin.accounts.usageWindow.estimatedTotalCost', { cost: cnyNumber(estimatedTotalCost) }) }}
        </span>
      </div>
    </div>

    <!-- Progress bar row -->
    <div class="flex items-center gap-1">
      <!-- Label badge (label-width: fixed = 定宽居中, auto = 限宽截断左对齐) -->
      <span :class="[labelSizeClass, labelClass]">
        {{ label }}
      </span>

      <!-- Progress bar container -->
      <div class="h-1.5 w-8 shrink-0 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
        <div
          :class="['h-full transition-all duration-300', barClass]"
          :style="{ width: barWidth }"
        ></div>
      </div>

      <!-- Percentage -->
      <span :class="['w-[32px] shrink-0 text-right text-[10px] font-medium', textClass]">
        {{ displayPercent }}
      </span>

      <!-- Reset time -->
      <span v-if="shouldShowResetTime" class="shrink-0 text-[10px] text-gray-400">
        {{ formatResetTime }}
      </span>
    </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { cnyNumber } from '@/utils/currency'

import { computed, inject, onBeforeUnmount, onMounted, onUnmounted, ref, useId, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { WindowStats } from '@/types'
import { formatCompactNumber } from '@/utils/format'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { accountUsageSummaryKey } from './usageSummary'

const props = withDefaults(
  defineProps<{
    label: string
    utilization: number // Percentage (0-100+)
    resetsAt?: string | null
    color: 'indigo' | 'emerald' | 'purple' | 'amber'
    windowStats?: WindowStats | null
    estimatedTotalCost?: number | null
    showNowWhenIdle?: boolean
    remainingCapacity?: boolean
    /** fixed: 定宽居中徽章（账号页纵向对齐）；auto: 限宽截断左对齐（监控页组合标签） */
    labelWidth?: 'fixed' | 'auto'
    detailed?: boolean
  }>(),
  { labelWidth: 'fixed' }
)

const { t, locale } = useI18n()

const fullLabel = computed(() => {
  const names: Record<string, string> = { '5h': 'fiveHours', '7d': 'sevenDays', '7d S': 'sonnetWeek', '7d F': 'fableWeek', '24h': 'twentyFourHours' }
  return names[props.label] ? t(`admin.accounts.usageWindow.${names[props.label]}`) : props.label
})
const windowExplanation = computed(() => `${fullLabel.value} · ${t(props.remainingCapacity ? 'admin.accounts.usageWindow.remainingHint' : 'admin.accounts.usageWindow.usedHint')}`)
const resetExplanation = computed(() => {
  const timestamp = props.resetsAt ? new Date(props.resetsAt) : null
  const exactTime = timestamp && Number.isFinite(timestamp.getTime())
    ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'long' }).format(timestamp)
    : t('admin.accounts.usageWindow.notReported')
  return `${t('admin.accounts.usageWindow.recoveryHint')}\n${t('admin.accounts.usageWindow.recoveryTime', { time: exactTime })}`
})

// Reactive clock for countdown — only runs when a reset time is shown,
// to avoid creating many idle timers across large account lists.
const now = ref(new Date())
const { pause: pauseClock, resume: resumeClock } = useIntervalFn(
  () => {
    now.value = new Date()
  },
  60_000,
  { immediate: false },
)
if (props.resetsAt) resumeClock()
const refreshClockWhenVisible = () => {
  if (!document.hidden && props.resetsAt) now.value = new Date()
}
onMounted(() => document.addEventListener('visibilitychange', refreshClockWhenVisible))
onUnmounted(() => document.removeEventListener('visibilitychange', refreshClockWhenVisible))
watch(
  () => props.resetsAt,
  (val) => {
    if (val) {
      now.value = new Date()
      resumeClock()
    } else {
      pauseClock()
    }
  },
)

// Label background colors
const labelClass = computed(() => {
  const colors = {
    indigo: 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300',
    emerald: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
    purple: 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
    amber: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }
  return colors[props.color]
})

// Label badge width mode: fixed 定宽保证账号页纵向对齐；auto 限宽截断适配
// 监控页「Pro/7 天」类组合标签。百分比列在两种模式下保持不变。
const labelSizeClass = computed(() =>
  props.labelWidth === 'auto'
    ? 'max-w-[72px] shrink-0 truncate rounded px-1 text-left text-[10px] font-medium'
    : 'w-[32px] shrink-0 rounded px-1 text-center text-[10px] font-medium'
)

// Progress bar color based on utilization
const barClass = computed(() => {
  if (props.remainingCapacity) {
    if (props.utilization <= 20) {
      return 'bg-red-500'
    } else if (props.utilization <= 50) {
      return 'bg-amber-500'
    }
    return 'bg-green-500'
  }
  if (props.utilization >= 90) {
    return 'bg-red-500'
  } else if (props.utilization >= 75) {
    return 'bg-amber-500'
  } else {
    return 'bg-green-500'
  }
})

// Text color based on utilization
const textClass = computed(() => {
  if (props.remainingCapacity) {
    if (props.utilization <= 20) {
      return 'text-red-600 dark:text-red-400'
    } else if (props.utilization <= 50) {
      return 'text-amber-600 dark:text-amber-400'
    }
    return 'text-gray-600 dark:text-gray-400'
  }
  if (props.utilization >= 90) {
    return 'text-red-600 dark:text-red-400'
  } else if (props.utilization >= 75) {
    return 'text-amber-600 dark:text-amber-400'
  } else {
    return 'text-gray-600 dark:text-gray-400'
  }
})

// Bar width (capped at 100%)
const barWidth = computed(() => {
  return `${Math.min(Math.max(props.utilization, 0), 100)}%`
})

// Display percentage (cap at 999% for readability)
const displayPercent = computed(() => {
  const percent = Math.round(
    props.remainingCapacity
      ? Math.min(Math.max(props.utilization, 0), 100)
      : props.utilization
  )
  return percent > 999 ? '>999%' : `${percent}%`
})

const summaries = inject(accountUsageSummaryKey, null)
// Accounts use the same measurement layout for every provider, including nested quota cells.
const detailed = computed(() => props.detailed ?? summaries !== null)
const summaryId = useId()
summaries?.set(summaryId, computed(() => ({
  label: fullLabel.value,
  caption: t(props.remainingCapacity ? 'admin.accounts.usageWindow.remaining' : 'admin.accounts.usageWindow.used'),
  percent: displayPercent.value,
  utilization: Math.min(Math.max(props.utilization, 0), 100),
})))
onBeforeUnmount(() => summaries?.delete(summaryId))

const shouldShowResetTime = computed(() => {
  if (props.resetsAt) return true
  return Boolean(props.showNowWhenIdle && props.utilization <= 0)
})

// Format reset time
const formatResetTime = computed(() => {
  if (!props.resetsAt) {
    // Idle rolling window without a known reset time: treat as immediately available.
    return props.showNowWhenIdle && props.utilization <= 0 ? t('usage.resetNow') : '-'
  }

  const date = new Date(props.resetsAt)
  const diffMs = date.getTime() - now.value.getTime()

  // resetsAt 已过期：utilization>0 说明后端窗口数据还没刷新（active poll 没回写），
  // 显示「待刷新」以区别于真正可用的「现在」。
  if (diffMs <= 0) {
    return props.utilization > 0 ? t('usage.resetPending') : t('usage.resetNow')
  }

  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffMins = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60))

  if (diffHours >= 24) {
    const days = Math.floor(diffHours / 24)
    return `${days}d ${diffHours % 24}h`
  } else if (diffHours > 0) {
    return `${diffHours}h ${diffMins}m`
  } else {
    return `${diffMins}m`
  }
})

// Window stats formatters
const formatRequests = computed(() => {
  if (!props.windowStats) return ''
  return formatCompactNumber(props.windowStats.requests, { allowBillions: false })
})

const formatTokens = computed(() => {
  if (!props.windowStats) return ''
  return formatCompactNumber(props.windowStats.tokens)
})

const formatAccountCost = computed(() => {
  if (!props.windowStats) return '0.00'
  return cnyNumber(props.windowStats.cost)
})

const formatUserCost = computed(() => {
  if (!props.windowStats || props.windowStats.user_cost == null) return '0.00'
  return cnyNumber(props.windowStats.user_cost)
})

</script>
