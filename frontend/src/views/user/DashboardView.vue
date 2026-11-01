<template>
  <AppLayout>
    <div class="space-y-6" data-testid="account-usage-page" :aria-busy="loading">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.title') }}</h1>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">{{ t('accountInsights.subtitle') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.scope') }}<span v-if="updatedAt"> · {{ t('accountInsights.updatedAt', { time: formatDateTime(updatedAt) }) }}</span></p>
        </div>
        <div class="flex w-full flex-wrap items-center justify-start gap-3 sm:w-auto sm:justify-end">
          <UserDashboardDatePresets :range="range" @update:range="setRange" />
          <UserDashboardRefreshControl :seconds="refreshControlSeconds" :countdown="countdown" :hidden="documentHidden" @update:seconds="setRefreshInterval" />
          <button type="button" class="btn btn-secondary" :aria-busy="loading" @click="refreshAll">{{ loading ? t('accountInsights.refreshing') : t('accountInsights.refresh') }}</button>
        </div>
      </header>

      <div v-if="error && snapshot" role="status" aria-live="polite" class="rounded-xl border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
        {{ t('accountInsights.refreshFailed') }} — {{ t('accountInsights.staleNotice', { start: snapshot.range.startDate, end: snapshot.range.endDate }) }}
        <span v-if="lastGoodAt"> {{ t('accountInsights.lastGoodAt', { time: formatDateTime(lastGoodAt) }) }}</span>
      </div>
      <div v-else-if="error" role="alert" class="rounded-xl border border-red-300 bg-red-50 p-3 text-sm text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-100">
        {{ t('accountInsights.loadFailed') }} <button type="button" class="ml-2 underline" @click="refreshAll">{{ t('accountInsights.common.retry') }}</button>
      </div>

      <PageLoading v-if="loading && !snapshot" />
      <template v-if="snapshot">
        <UserDashboardStats
          ref="stats"
          :profile="snapshot.profile"
          :dashboard-stats="snapshot.dashboardStats"
          :range-stats="snapshot.rangeStats"
          :rolling30="snapshot.rolling30"
          :quotas="snapshot.quotas"
          :health="snapshot.health"
          :low-balance="snapshot.lowBalance"
        />
        <UserDashboardCharts
          :loading="loading"
          :range="snapshot.range"
          :trend="snapshot.trend"
          :models="snapshot.models"
          :groups="snapshot.groups"
          :range-stats="snapshot.rangeStats"
        />
        <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
          <UserDashboardKeyRanking
            :rows="snapshot.keyRows"
            :loading="keyLoading"
            :selected-key-id="selectedKeyId"
            :truncated="snapshot.keyRowsTruncated"
            @select-key="openKeyDrilldown"
            @sort-change="setKeySort"
            @export-csv="exportCsv"
          />
          <UserDashboardActiveNow
            :windows="snapshot.activeNow"
            :selected-window="activeWindow"
            :loading="activeNowLoading"
            @update:selected-window="activeWindow = $event"
          />
        </div>
        <UserDashboardKeyDrilldown
          v-if="selectedKeyId !== null"
          :key-info="selectedKeyInfo"
          :daily="keyDrilldown.daily"
          :models="keyDrilldown.models"
          :loading="keyDrilldown.loading"
          :daily-loading="keyDrilldown.dailyLoading"
          :models-loading="keyDrilldown.modelsLoading"
          :daily-error="keyDrilldown.dailyError"
          :models-error="keyDrilldown.modelsError"
          @close="closeKeyDrilldown"
        />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <UserDashboardConsistency class="lg:col-span-2" :summary="snapshot.consistency" :loading="rollingLoading" />
          <div data-testid="account-errors-row" class="lg:col-span-2">
            <UserDashboardErrors
              :items="snapshot.errors"
              :loading="errorsLoading"
              :unavailable="errorsUnavailable"
              @select-error="openErrorDetail"
            />
          </div>
        </div>
        <section
          v-if="errorDetailLoading || errorDetail || errorDetailError"
          class="card relative p-4"
          data-testid="account-error-detail"
          aria-live="polite"
        >
          <div class="mb-4 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.errors.detailTitle') }}</h3>
            <button type="button" class="btn btn-secondary btn-sm" @click="closeErrorDetail">{{ t('accountInsights.common.close') }}</button>
          </div>
          <p v-if="errorDetailLoading" class="text-sm text-gray-500 dark:text-gray-400">{{ t('accountInsights.common.loading') }}</p>
          <p v-else-if="errorDetailError" role="alert" class="text-sm text-red-700 dark:text-red-300">{{ t('accountInsights.errors.loadFailed') }}</p>
          <div v-else-if="errorDetail" class="space-y-4 text-sm">
            <dl class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <div><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.status') }}</dt><dd class="mt-0.5 text-gray-900 dark:text-white">{{ errorDetail.status_code || '-' }}</dd></div>
              <div><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.category') }}</dt><dd class="mt-0.5 break-words text-gray-900 dark:text-white">{{ errorDetail.category || '-' }}</dd></div>
              <div><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.endpoint') }}</dt><dd class="mt-0.5 break-all text-gray-900 dark:text-white">{{ errorDetail.inbound_endpoint || '-' }}</dd></div>
              <div><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.model') }}</dt><dd class="mt-0.5 break-words text-gray-900 dark:text-white">{{ errorDetail.model || '-' }}</dd></div>
              <div class="sm:col-span-2"><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.message') }}</dt><dd class="mt-0.5 break-words text-gray-900 dark:text-white">{{ errorDetail.message || '-' }}</dd></div>
              <div v-if="errorDetail.upstream_status_code != null"><dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.upstreamStatus') }}</dt><dd class="mt-0.5 text-gray-900 dark:text-white">{{ errorDetail.upstream_status_code }}</dd></div>
            </dl>
            <div v-if="errorDetail.error_body" class="border-t border-gray-200 pt-3 dark:border-dark-700">
              <button type="button" class="text-sm font-medium text-primary-600 underline dark:text-primary-400" data-testid="account-error-body-toggle" @click="errorDetailBodyVisible = !errorDetailBodyVisible">
                {{ errorDetailBodyVisible ? t('accountInsights.errors.hideBody') : t('accountInsights.errors.showBody') }}
              </button>
              <pre v-if="errorDetailBodyVisible" data-testid="account-error-body" class="mt-2 max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200">{{ errorDetail.error_body }}</pre>
            </div>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as DashboardStats } from '@/api/usage'
import { getProfile, getMyPlatformQuotas } from '@/api/user'
import { keysAPI } from '@/api/keys'
import { currencyRate, toCny } from '@/utils/currency'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import PageLoading from '@/components/common/PageLoading.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardDatePresets from '@/components/user/dashboard/core/UserDashboardDatePresets.vue'
import UserDashboardRefreshControl from '@/components/user/dashboard/core/UserDashboardRefreshControl.vue'
import UserDashboardKeyRanking from '@/components/user/dashboard/insights/UserDashboardKeyRanking.vue'
import UserDashboardKeyDrilldown from '@/components/user/dashboard/insights/UserDashboardKeyDrilldown.vue'
import UserDashboardActiveNow from '@/components/user/dashboard/insights/UserDashboardActiveNow.vue'
import UserDashboardConsistency from '@/components/user/dashboard/insights/UserDashboardConsistency.vue'
import UserDashboardErrors from '@/components/user/dashboard/insights/UserDashboardErrors.vue'
import { aggregateActiveNow, aggregateKeyUsage, aggregateRollingAndConsistency, deriveDashboardHealth } from '@/components/user/dashboard/insights/aggregate'
import { exportDashboardCsv } from '@/components/user/dashboard/insights/exportCsv'
import type { DashboardRange, DashboardHealth, DashboardKeyUsageRow, DashboardRolling30d, ActiveNowWindow, DashboardConsistency, DashboardErrorRow, DashboardKeyDrilldownState } from '@/components/user/dashboard/insights/types'
import type { ApiKey, GroupStat, ModelStat, PlatformQuotaItem, TrendDataPoint, UsageLog, UsageStatsResponse, User } from '@/types'

const { t } = useI18n()
const authStore = useAuthStore()
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
const loading = ref(false)
const keyLoading = ref(false)
const activeNowLoading = ref(false)
const rollingLoading = ref(false)
const errorsLoading = ref(false)
const errorsUnavailable = ref(false)
const errorDetail = ref<import('@/types').UserErrorRequestDetail | null>(null)
const errorDetailLoading = ref(false)
const errorDetailError = ref(false)
const errorDetailBodyVisible = ref(false)
let errorDetailRequestVersion = 0
let refreshQueued = false
const error = ref(false)
const updatedAt = ref('')
const lastGoodAt = ref('')
const selectedKeyId = ref<number | null>(null)
const activeWindow = ref<'60m' | '12h' | '24h'>('60m')
const keySort = ref<'cost' | 'calls' | 'tokens' | 'favouriteModel' | 'lastUsedAt'>('cost')
const range = ref<DashboardRange>(makePreset('7d'))
const requestSequence = ref(0)
const documentHidden = ref(typeof document !== 'undefined' ? document.hidden : false)
const refreshSeconds = ref(30)
const countdown = ref(30)
let timer: ReturnType<typeof setInterval> | null = null
let abortController: AbortController | null = null
let dismissedListener: (() => void) | null = null

interface DashboardSnapshot {
  profile: User
  dashboardStats: DashboardStats
  range: DashboardRange
  rangeStats: UsageStatsResponse | null
  rolling30: DashboardRolling30d | null
  quotas: PlatformQuotaItem[]
  trend: TrendDataPoint[]
  models: ModelStat[]
  groups: GroupStat[]
  keyRows: DashboardKeyUsageRow[]
  keyRowsTruncated: boolean
  activeNow: ActiveNowWindow[]
  consistency: DashboardConsistency | null
  errors: DashboardErrorRow[]
  health: DashboardHealth
  lowBalance: { visible: boolean; thresholdCny: number; dismissed: boolean }
  keys: ApiKey[]
}
const snapshot = ref<DashboardSnapshot | null>(null)
const selectedKeyInfo = computed<ApiKey | null>(() => snapshot.value?.keys.find((key) => key.id === selectedKeyId.value) ?? null)
const keyDrilldown = ref<DashboardKeyDrilldownState>({ keyInfo: null, daily: null, models: [], loading: false, dailyLoading: false, modelsLoading: false, dailyError: false, modelsError: false })
const refreshControlSeconds = computed(() => refreshSeconds.value)

function pad(value: number): string { return String(value).padStart(2, '0') }
function localDate(date: Date): string { return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` }
function shiftDate(date: Date, amount: number): Date { const next = new Date(date); next.setDate(next.getDate() + amount); return next }
function monday(date: Date): Date { const day = date.getDay(); return shiftDate(date, day === 0 ? -6 : 1 - day) }
function makePreset(preset: DashboardRange['preset'], now = new Date()): DashboardRange {
  const today = localDate(now)
  let start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  let end = new Date(start)
  if (preset === '7d') start = shiftDate(start, -6)
  else if (preset === '30d') start = shiftDate(start, -29)
  else if (preset === 'thisWeek') start = monday(start)
  else if (preset === 'lastWeek') { end = shiftDate(monday(start), -1); start = shiftDate(end, -6) }
  else if (preset === 'thisMonth') start = new Date(start.getFullYear(), start.getMonth(), 1)
  return { startDate: localDate(start), endDate: preset === 'lastWeek' ? localDate(end) : today, timezone, preset }
}
function validateRange(value: DashboardRange): boolean {
  const start = Date.parse(`${value.startDate}T00:00:00`)
  const end = Date.parse(`${value.endDate}T00:00:00`)
  return Number.isFinite(start) && Number.isFinite(end) && end >= start && Math.floor((end - start) / 86400000) + 1 <= 30
}
function setRange(value: DashboardRange) {
  const next = { ...value, timezone }
  if (next.preset !== 'custom') Object.assign(next, makePreset(next.preset))
  range.value = next
  if (validateRange(next)) void refreshAll()
}
function last30Range(): DashboardRange { return makePreset('30d') }
function userStorage(name: string, id = authStore.user?.id): string { return `account-insights:${name}:${id ?? 'anonymous'}` }
function loadRefreshPreference() {
  const stored = localStorage.getItem(userStorage('refresh-interval'))
  if (stored !== null) {
    const saved = Number(stored)
    if ([0, 30, 60, 300, 900].includes(saved)) refreshSeconds.value = saved
  } else {
    const legacy = localStorage.getItem(`account-usage-auto-refresh:${authStore.user?.id ?? 'anonymous'}`)
    refreshSeconds.value = legacy === 'false' ? 0 : 30
  }
  countdown.value = refreshSeconds.value
}
function saveRefreshPreference() { localStorage.setItem(userStorage('refresh-interval'), String(refreshSeconds.value)) }
function setRefreshInterval(seconds: number) { refreshSeconds.value = [0, 30, 60, 300, 900].includes(seconds) ? seconds : 0; saveRefreshPreference(); countdown.value = refreshSeconds.value; restartTimer() }
function restartTimer() { if (timer) clearInterval(timer); timer = null; if (refreshSeconds.value > 0) timer = setInterval(onTimerTick, 1000) }
function onTimerTick() { if (documentHidden.value || loading.value || refreshSeconds.value <= 0) return; countdown.value -= 1; if (countdown.value <= 0) { countdown.value = refreshSeconds.value; void refreshAll() } }
function isCurrent(sequence: number, accountId: number): boolean { return sequence === requestSequence.value && authStore.user?.id === accountId }
function todayAndYesterday(): { start_date: string; end_date: string; timezone: string } { const now = new Date(); return { start_date: localDate(shiftDate(now, -1)), end_date: localDate(now), timezone } }
function numeric(value: unknown): number { const parsed = Number(value); return Number.isFinite(parsed) ? parsed : 0 }
function sortRows(rows: DashboardKeyUsageRow[]): DashboardKeyUsageRow[] {
  const compareText = (a: string | null, b: string | null) => (a === null ? 1 : b === null ? -1 : a.localeCompare(b))
  return [...rows].sort((a, b) => {
    if (keySort.value === 'cost') return numeric(b.selectedRangeCostUsd) - numeric(a.selectedRangeCostUsd) || a.apiKeyId - b.apiKeyId
    if (keySort.value === 'calls') return numeric(b.requests) - numeric(a.requests) || a.apiKeyId - b.apiKeyId
    if (keySort.value === 'tokens') return numeric(b.totalTokens) - numeric(a.totalTokens) || a.apiKeyId - b.apiKeyId
    if (keySort.value === 'favouriteModel') return compareText(a.favouriteModel, b.favouriteModel) || a.apiKeyId - b.apiKeyId
    return compareText(b.lastUsedAt, a.lastUsedAt) || a.apiKeyId - b.apiKeyId
  })
}
function setKeySort(value: typeof keySort.value) { keySort.value = value; if (snapshot.value) snapshot.value.keyRows = sortRows(snapshot.value.keyRows) }
function exportCsv() { if (!snapshot.value) return; exportDashboardCsv({ range: snapshot.value.range, rangeStats: snapshot.value.rangeStats, models: snapshot.value.models, keys: snapshot.value.keyRows }) }
async function fetchPages(params: Record<string, unknown>, signal: AbortSignal): Promise<{ rows: UsageLog[]; pages: number; hasMore: boolean }> {
  const rows: UsageLog[] = []; let page = 1; let hasMore = false
  for (; page <= 5; page += 1) {
    const response = await usageAPI.query({ ...params, page, page_size: 1000, sort_by: 'created_at', sort_order: 'desc' }, { signal })
    rows.push(...(response.items ?? []))
    hasMore = Number(response.pages ?? page) > page || Boolean((response as { has_more?: boolean }).has_more)
    if (!hasMore) break
  }
  return { rows, pages: page, hasMore }
}
function profileLowBalance(profile: User) {
  const threshold = Number(localStorage.getItem(userStorage('warning-threshold')) ?? 210)
  const thresholdCny = Number.isFinite(threshold) && threshold >= 0 ? threshold : 210
  const dismissed = localStorage.getItem(userStorage('warning-dismissed')) === '1'
  const visible = Boolean(currencyRate.value) && Number.isFinite(toCny(profile.balance)) && toCny(profile.balance) <= thresholdCny
  return { visible, thresholdCny, dismissed }
}
async function refreshAll() {
  if (loading.value) {
    refreshQueued = true
    return
  }
  const accountId = authStore.user?.id
  if (accountId == null || !validateRange(range.value)) return
  abortController?.abort(); abortController = new AbortController()
  const signal = abortController.signal
  const sequence = ++requestSequence.value
  loading.value = true; keyLoading.value = true; activeNowLoading.value = true; rollingLoading.value = true; errorsLoading.value = true
  const selected = { start_date: range.value.startDate, end_date: range.value.endDate, timezone: range.value.timezone }
  const rolling = last30Range()
  const activeEnvelope = todayAndYesterday()
  const results = await Promise.allSettled([
    getProfile({ signal }),
    usageAPI.getDashboardStats({ signal }),
    usageAPI.getStatsByRange(selected, { signal }),
    usageAPI.getDashboardSnapshotV2({ ...selected, granularity: range.value.preset === 'today' ? 'hour' : 'day', include_trend: true, include_model_stats: true, include_group_stats: true }, { signal }),
    usageAPI.query({ ...selected, page: 1, page_size: 10, sort_by: 'created_at', sort_order: 'desc' }, { signal }),
    keysAPI.list(1, 1000, undefined, { signal }),
    getMyPlatformQuotas({ signal }),
    usageAPI.getStatsByRange({ start_date: rolling.startDate, end_date: rolling.endDate, timezone: rolling.timezone }, { signal }),
    usageAPI.getDashboardTrend({ start_date: rolling.startDate, end_date: rolling.endDate, timezone: rolling.timezone, granularity: 'day' }, { signal }),
    fetchPages(selected, signal),
    fetchPages(activeEnvelope, signal),
    usageAPI.listMyErrorRequests({ ...selected, page: 1, page_size: 10, sort_by: 'created_at', sort_order: 'desc' }, { signal }),
  ])
  if (!isCurrent(sequence, accountId) || signal.aborted) return
  const profileResult = results[0]; const statsResult = results[1]; const rangeResult = results[2]
  if (profileResult.status !== 'fulfilled' || statsResult.status !== 'fulfilled' || rangeResult.status !== 'fulfilled' || profileResult.value.id !== accountId) {
    error.value = true; loading.value = false; keyLoading.value = false; activeNowLoading.value = false; rollingLoading.value = false; errorsLoading.value = false
    if (refreshQueued) { refreshQueued = false; void refreshAll() }
    return
  }
  const profile = profileResult.value; const dashboardStats = statsResult.value; const rangeStats = rangeResult.value
  const snapshotResult = results[3].status === 'fulfilled' ? results[3].value : null
  const keysResult = results[5].status === 'fulfilled' ? results[5].value.items : []
  const quotas = results[6].status === 'fulfilled' ? results[6].value.platform_quotas ?? [] : []
  const rollingStats = results[7].status === 'fulfilled' ? results[7].value : null
  const rollingTrend = results[8].status === 'fulfilled' ? results[8].value.trend ?? [] : []
  const keyScan = results[9].status === 'fulfilled' ? results[9].value : { rows: [], pages: 0, hasMore: false }
  const activeScan = results[10].status === 'fulfilled' ? results[10].value : { rows: [], pages: 0, hasMore: false }
  const batchStats: Record<string, { api_key_id: number; today_actual_cost: number; total_actual_cost: number }> = {}
  if (keysResult.length) {
    for (let offset = 0; offset < keysResult.length; offset += 100) {
      const response = await usageAPI.getDashboardApiKeysUsage(keysResult.slice(offset, offset + 100).map((key) => key.id), { signal })
      Object.assign(batchStats, response.stats)
    }
  }
  if (!isCurrent(sequence, accountId) || signal.aborted) return
  const keyRows = sortRows(aggregateKeyUsage(keyScan.rows, keysResult, batchStats, { maxPages: 5, pagesFetched: keyScan.pages, hasMore: keyScan.hasMore }))
  const activeNow = aggregateActiveNow(activeScan.rows, { maxPages: 5, pagesFetched: activeScan.pages, hasMore: activeScan.hasMore })
  const { rolling30, consistency } = aggregateRollingAndConsistency(rolling.startDate, rolling.endDate, rollingStats, rollingTrend)
  const failedOptional = results.some((result, index) => result.status === 'rejected' && index !== 4 && index !== 5 && index !== 6 && index !== 11)
  const quotaAttention = quotas.some((quota) => (['daily', 'weekly', 'monthly'] as const).some((window) => { const limit = quota[`${window}_limit_usd`]; return limit != null && limit > 0 && numeric(quota[`${window}_usage_usd`]) / limit >= 0.8 }))
  const lowBalance = profileLowBalance(profile)
  const requiredFields = [
    dashboardStats.total_requests,
    dashboardStats.total_tokens,
    dashboardStats.today_actual_cost,
    dashboardStats.rpm,
    dashboardStats.tpm,
    rangeStats.total_requests,
    rangeStats.total_tokens,
    rangeStats.total_actual_cost,
    rangeStats.average_duration_ms,
  ]
  const requiredFailed = requiredFields.some((value) => !Number.isFinite(Number(value)))
  const health = deriveDashboardHealth({ loaded: true, stale: failedOptional, lowBalance: lowBalance.visible && !lowBalance.dismissed, truncated: keyScan.hasMore || activeScan.hasMore, quotaAttention, requiredFailed })
  const errorResult = results[11]
  errorsUnavailable.value = errorResult.status === 'rejected'
  const errors = errorResult.status === 'fulfilled' ? errorResult.value.items.map((item) => ({ id: item.id, created_at: item.created_at, model: item.model, inbound_endpoint: item.inbound_endpoint, status_code: item.status_code, category: item.category, platform: item.platform, message: item.message, key_name: item.key_name })) : []
  snapshot.value = { profile, dashboardStats, range: { ...range.value }, rangeStats, rolling30, quotas, trend: snapshotResult?.trend ?? [], models: snapshotResult?.models ?? [], groups: snapshotResult?.groups ?? [], keyRows, keyRowsTruncated: keyScan.hasMore, activeNow, consistency, errors, health, lowBalance, keys: keysResult }
  authStore.user = profile; updatedAt.value = new Date().toISOString(); lastGoodAt.value = updatedAt.value; error.value = failedOptional
  loading.value = false; keyLoading.value = false; activeNowLoading.value = false; rollingLoading.value = false; errorsLoading.value = false; countdown.value = refreshSeconds.value
  if (refreshQueued) { refreshQueued = false; void refreshAll() }
}
async function openKeyDrilldown(apiKeyId: number) {
  selectedKeyId.value = apiKeyId
  keyDrilldown.value = {
    keyInfo: selectedKeyInfo.value,
    daily: null,
    models: [],
    loading: true,
    dailyLoading: true,
    modelsLoading: true,
    dailyError: false,
    modelsError: false,
  }

  const dailyRequest = usageAPI.getMyApiKeyDailyUsage(apiKeyId, 30, timezone)
  const modelsRequest = usageAPI.getDashboardModels({
    api_key_id: apiKeyId,
    start_date: range.value.startDate,
    end_date: range.value.endDate,
    timezone: range.value.timezone,
    model_source: 'requested',
  })
  const [dailyResult, modelsResult] = await Promise.allSettled([dailyRequest, modelsRequest])
  if (selectedKeyId.value !== apiKeyId) return

  keyDrilldown.value = {
    keyInfo: selectedKeyInfo.value,
    daily: dailyResult.status === 'fulfilled' ? dailyResult.value : null,
    models: modelsResult.status === 'fulfilled' ? modelsResult.value.models ?? [] : [],
    loading: false,
    dailyLoading: false,
    modelsLoading: false,
    dailyError: dailyResult.status === 'rejected',
    modelsError: modelsResult.status === 'rejected',
  }
}
function closeKeyDrilldown() { selectedKeyId.value = null; keyDrilldown.value = { keyInfo: null, daily: null, models: [], loading: false, dailyLoading: false, modelsLoading: false, dailyError: false, modelsError: false } }
async function openErrorDetail(id: number) {
  const version = ++errorDetailRequestVersion
  errorDetailBodyVisible.value = false
  errorDetail.value = null
  errorDetailError.value = false
  errorDetailLoading.value = true
  try {
    const detail = await usageAPI.getMyErrorDetail(id)
    if (version === errorDetailRequestVersion) errorDetail.value = detail
  } catch {
    if (version === errorDetailRequestVersion) errorDetailError.value = true
  } finally {
    if (version === errorDetailRequestVersion) errorDetailLoading.value = false
  }
}
function closeErrorDetail() {
  errorDetailRequestVersion += 1
  errorDetail.value = null
  errorDetailLoading.value = false
  errorDetailError.value = false
  errorDetailBodyVisible.value = false
}
function onVisibilityChange() { documentHidden.value = document.hidden; if (!document.hidden && refreshSeconds.value > 0 && !loading.value) { countdown.value = refreshSeconds.value; void refreshAll() } }
function onDismissed() { if (snapshot.value) snapshot.value.lowBalance.dismissed = true }
watch(() => authStore.user?.id, (id, previous) => { if (id === previous) return; ++requestSequence.value; abortController?.abort(); snapshot.value = null; selectedKeyId.value = null; if (id != null) { loadRefreshPreference(); restartTimer(); void refreshAll() } })
onMounted(() => { loadRefreshPreference(); dismissedListener = onDismissed; window.addEventListener('account-insights-warning-dismissed', dismissedListener); document.addEventListener('visibilitychange', onVisibilityChange); restartTimer(); void refreshAll() })
onBeforeUnmount(() => { ++requestSequence.value; abortController?.abort(); errorDetailRequestVersion += 1; if (timer) clearInterval(timer); timer = null; document.removeEventListener('visibilitychange', onVisibilityChange); if (dismissedListener) window.removeEventListener('account-insights-warning-dismissed', dismissedListener) })
</script>
