import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { useAuthStore } from './auth'
import { useAppStore } from './app'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { getAvailable, type UserAvailableChannel } from '@/api/channels'
import { getUserGroupRates } from '@/api/groups'
import * as monitor from '@/api/channelMonitorV2'

export const useModelStatusStore = defineStore('model-status', () => {
  const auth = useAuthStore()
  const app = useAppStore()
  const catalog = ref<ModelPlazaResponse | null>(null)
  const channels = ref<UserAvailableChannel[]>([])
  const rates = ref<Record<number, number>>({})
  const snapshot = ref<monitor.MonitorSnapshot | null>(null)
  const models = ref<monitor.MonitorModelRow[]>([])
  const matrix = ref<monitor.MonitorMatrixResponse | null>(null)
  const dimensions = ref<monitor.MonitorDimensions>({ platforms: [], groups: [], models: [] })
  const filter = ref<monitor.MonitorFilter>({ range: '24h', platforms: [], groupIds: [], models: [] })
  const groupBy = ref<monitor.MonitorMatrixGroupBy>('platform_group')
  const loading = ref(false)
  const catalogError = ref(false)
  const channelsError = ref(false)
  const statusError = ref(false)
  const catalogUpdatedAt = ref('')
  const channelsUpdatedAt = ref('')
  const interval = computed(() => snapshot.value?.config.refresh_interval_seconds || 60)
  const monitorEnabled = computed(() => app.cachedPublicSettings?.channel_monitor_enabled === true && app.cachedPublicSettings?.channel_monitor_mode === 'v2')
  // An empty permission-scoped response can contain Go's zero timestamp.
  const hasObservation = computed(() => Boolean(snapshot.value && Date.parse(snapshot.value.coverage.data_through) > 0))
  const stale = computed(() => statusError.value || Boolean(hasObservation.value && snapshot.value && (
    snapshot.value.coverage.aggregation_lag_seconds > interval.value * 2 ||
    Date.now() - Date.parse(snapshot.value.coverage.data_through) > interval.value * 2000
  )))
  let clients = 0
  let generation = 0
  let request: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | null = null

  function reset() {
    generation++
    request?.abort()
    request = null
    loading.value = false
    catalog.value = null
    channels.value = []
    rates.value = {}
    snapshot.value = null
    models.value = []
    matrix.value = null
    dimensions.value = { platforms: [], groups: [], models: [] }
    catalogUpdatedAt.value = channelsUpdatedAt.value = ''
    catalogError.value = channelsError.value = statusError.value = false
  }
  function schedule() {
    if (timer) clearTimeout(timer)
    timer = null
    if (clients && !document.hidden) timer = setTimeout(() => void refresh(), interval.value * 1000)
  }
  async function refresh(force = false) {
    if (loading.value && !force) return
    request?.abort()
    const controller = new AbortController()
    request = controller
    const id = ++generation
    loading.value = true
    const signal = controller.signal
    const current = { ...filter.value, platforms: [...filter.value.platforms], groupIds: [...filter.value.groupIds], models: [...filter.value.models] }
    const tasks: Promise<unknown>[] = []
    const add = <T>(promise: Promise<T>, commit: (data: T) => void, fail: () => void) => {
      tasks.push(promise.then(data => { if (id === generation) commit(data) }).catch(error => {
        if (id !== generation || signal.aborted) return
        if ([401, 403].includes(error?.response?.status ?? error?.status)) { reset(); catalogError.value = channelsError.value = statusError.value = true; return }
        fail()
      }))
    }
    if (app.cachedPublicSettings?.model_plaza_enabled && (auth.isAuthenticated || !app.cachedPublicSettings?.model_plaza_require_auth)) add(getModelPlaza({ signal }), data => {
      catalog.value = data; catalogError.value = false; catalogUpdatedAt.value = new Date().toISOString()
    }, () => { catalogError.value = true })
    if (auth.isAuthenticated && app.cachedPublicSettings?.available_channels_enabled) add(Promise.all([
      getAvailable({ signal }), getUserGroupRates({ signal })
    ]), ([list, overrides]) => {
      channels.value = list; rates.value = overrides; channelsError.value = false; channelsUpdatedAt.value = new Date().toISOString()
    }, () => { channelsError.value = true })
    if (auth.isAuthenticated && monitorEnabled.value) add(Promise.all([
      monitor.getSnapshot(current, false, signal), monitor.getModels(current, false, signal),
      monitor.getMatrix(current, groupBy.value, false, signal),
      monitor.getDimensions({ ...current, platforms: [], groupIds: [], models: [] }, false, signal)
    ]), ([summary, rows, grid, choices]) => {
      snapshot.value = summary; models.value = rows.items; matrix.value = grid; dimensions.value = choices; statusError.value = false
    }, () => { statusError.value = true })
    await Promise.allSettled(tasks)
    if (id === generation) { loading.value = false; schedule() }
  }
  function visibility() {
    if (document.hidden) { if (timer) clearTimeout(timer); timer = null; generation++; request?.abort(); loading.value = false }
    else void refresh(true)
  }
  function retain() {
    if (++clients === 1) { document.addEventListener('visibilitychange', visibility); void refresh(true) }
  }
  function release() {
    if (clients > 0 && --clients === 0) {
      document.removeEventListener('visibilitychange', visibility)
      if (timer) clearTimeout(timer)
      timer = null; generation++; request?.abort(); loading.value = false
    }
  }
  function findModel(platform: string, model: string) {
    return models.value.find(row => row.platform === platform && row.model === model)
  }
  watch(() => auth.token, () => { reset(); if (clients) void refresh(true) }, { flush: 'sync' })
  watch([filter, groupBy], () => { if (clients) void refresh(true) }, { deep: true })
  return { catalog, channels, rates, snapshot, models, matrix, dimensions, filter, groupBy, loading,
    catalogError, channelsError, statusError, catalogUpdatedAt, channelsUpdatedAt, monitorEnabled, hasObservation, interval, stale,
    findModel, refresh, retain, release, reset }
})
