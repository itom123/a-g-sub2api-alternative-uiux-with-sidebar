<template>
  <AppLayout>
    <div class="space-y-6" data-testid="account-usage-page" :aria-busy="loading">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="text-sm text-gray-600 dark:text-gray-300" aria-live="polite">
          <p>{{ t('dashboard.accountScope') }}</p>
          <p v-if="updatedAt" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.updatedAt', { time: formatDateTime(updatedAt) }) }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-3 text-sm">
          <label class="flex cursor-pointer items-center gap-2">
            <input type="checkbox" class="rounded" :checked="autoEnabled" @change="setAutoEnabled(($event.target as HTMLInputElement).checked)" />
            {{ t('dashboard.autoRefresh') }}
          </label>
          <span v-if="autoEnabled" class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.refreshCountdown', { seconds: countdown }) }}
          </span>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="refreshAll">
            {{ loading ? t('common.loading') : t('common.refresh') }}
          </button>
        </div>
      </div>

      <div v-if="error" role="alert" class="rounded-xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
        {{ snapshot ? t('dashboard.refreshFailed', { start: snapshot.start, end: snapshot.end }) : t('dashboard.loadFailed') }}
      </div>
      <PageLoading v-if="loading && !snapshot" />
      <template v-if="snapshot">
        <UserDashboardStats :stats="snapshot.stats" :balance="snapshot.user.balance" :frozen-balance="snapshot.user.frozen_balance" :is-simple="authStore.isSimpleMode" :platform-quotas="snapshot.quotas" />
        <UserDashboardCharts
          v-model:startDate="startDate"
          v-model:endDate="endDate"
          v-model:granularity="granularity"
          :loading="loading"
          :trend="snapshot.trend"
          :models="snapshot.models"
          @dateRangeChange="changeRange"
          @granularityChange="refreshAll"
        />
        <UserDashboardRecentUsage :data="snapshot.logs" :loading="loading" :start-date="snapshot.start" :end-date="snapshot.end" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { getProfile, getMyPlatformQuotas } from '@/api/user'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import AppLayout from '@/components/layout/AppLayout.vue'
import PageLoading from '@/components/common/PageLoading.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import { formatDateLocalInput, formatDateTime } from '@/utils/format'
import type { User, UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'

const { t } = useI18n()
const authStore = useAuthStore()
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref<'day' | 'hour'>('day')
const loading = ref(false)
const error = ref(false)
const updatedAt = ref('')
const snapshot = ref<{
  user: User
  stats: UserStatsType
  trend: TrendDataPoint[]
  models: ModelStat[]
  logs: UsageLog[]
  quotas: PlatformQuotaItem[]
  start: string
  end: string
} | null>(null)
let requestSequence = 0
let followToday = true

async function refreshAll() {
  if (followToday) {
    startDate.value = formatDateLocalInput(new Date(Date.now() - 6 * 86400000))
    endDate.value = formatDateLocalInput(new Date())
  }
  const sequence = ++requestSequence
  const userId = authStore.user?.id
  const range = { start_date: startDate.value, end_date: endDate.value }
  loading.value = true
  try {
    const [user, stats, trend, models, logs, quotas] = await Promise.all([
      getProfile(),
      usageAPI.getDashboardStats(),
      usageAPI.getDashboardTrend({ ...range, granularity: granularity.value }),
      usageAPI.getDashboardModels(range),
      usageAPI.query({ ...range, page: 1, page_size: 10, sort_by: 'created_at', sort_order: 'desc' }),
      getMyPlatformQuotas(),
    ])
    if (sequence !== requestSequence || userId !== authStore.user?.id || user.id !== userId) return
    snapshot.value = { user, stats, trend: trend.trend || [], models: models.models || [], logs: logs.items,
      quotas: quotas.platform_quotas ?? [], start: range.start_date, end: range.end_date }
    authStore.user = user
    updatedAt.value = new Date().toISOString()
    error.value = false
  } catch {
    if (sequence === requestSequence) error.value = true
  } finally {
    if (sequence === requestSequence) {
      loading.value = false
      resetCountdown()
    }
  }
}

function changeRange() {
  followToday = false
  void refreshAll()
}

const { enabled: autoEnabled, countdown, setEnabled: setAutoEnabled, start, resetCountdown } = useAutoRefresh({
  storageKey: `account-usage-auto-refresh:${authStore.user?.id}`,
  intervals: [30],
  defaultInterval: 30,
  defaultEnabled: true,
  onRefresh: refreshAll,
  shouldPause: () => document.hidden || loading.value,
})

watch(() => authStore.user?.id, (id) => {
  ++requestSequence
  snapshot.value = null
  updatedAt.value = ''
  if (id) void refreshAll()
})
function resumeVisiblePage() {
  if (!document.hidden && autoEnabled.value && !loading.value) void refreshAll()
}
onMounted(() => {
  void refreshAll()
  resetCountdown()
  if (autoEnabled.value) start()
  document.addEventListener('visibilitychange', resumeVisiblePage)
})
onBeforeUnmount(() => {
  ++requestSequence
  document.removeEventListener('visibilitychange', resumeVisiblePage)
})
</script>
