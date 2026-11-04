/** Shared self-scoped subscription snapshot, cache and refresh lifecycle. */
import { defineStore } from 'pinia'
import { ref, computed, onScopeDispose } from 'vue'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import { getSubscriptionAllowances } from '@/utils/subscriptionQuota'

const CACHE_TTL_MS = 60_000
const POLL_INTERVAL_MS = 5 * 60_000
const MAX_TIMEOUT_MS = 2_147_483_647

export const useSubscriptionStore = defineStore('subscriptions', () => {
  // Keep the last successful /active response intact: its windows are normalized by the backend.
  const activeSubscriptions = ref<UserSubscription[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastFetchedAt = ref<number | null>(null)
  const currentTime = ref(Date.now())

  let requestGeneration = 0
  let activePromise: Promise<UserSubscription[]> | null = null
  let pollerInterval: ReturnType<typeof setInterval> | null = null
  let expiryTimeout: ReturnType<typeof setTimeout> | null = null
  let lastAttemptAt: number | null = null

  const currentSubscriptions = computed(() => activeSubscriptions.value.filter((subscription) => {
    if (subscription.status !== 'active') return false
    const expiresAt = subscription.expires_at ? Date.parse(subscription.expires_at) : NaN
    return !Number.isFinite(expiresAt) || expiresAt > currentTime.value
  }))
  const allowances = computed(() => getSubscriptionAllowances(currentSubscriptions.value))
  const hasActiveSubscriptions = computed(() => currentSubscriptions.value.length > 0)

  function scheduleExpiryRefresh() {
    if (expiryTimeout) clearTimeout(expiryTimeout)
    expiryTimeout = null
    const now = Date.now()
    const expiries = activeSubscriptions.value
      .filter((subscription) => subscription.status === 'active')
      .map((subscription) => Date.parse(subscription.expires_at || ''))
      .filter((expiry) => Number.isFinite(expiry) && expiry > now)
    if (!expiries.length) return
    expiryTimeout = setTimeout(() => {
      expiryTimeout = null
      currentTime.value = Date.now()
      // Remove expired allowances reactively before the network response arrives.
      scheduleExpiryRefresh()
      if (typeof document === 'undefined' || document.visibilityState !== 'hidden') {
        fetchActiveSubscriptions(true).catch(() => {})
      }
    }, Math.min(MAX_TIMEOUT_MS, Math.max(1, Math.min(...expiries) - now)))
  }

  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    // A manual refresh, header and dashboard still share one network request.
    if (activePromise) return activePromise
    const now = Date.now()
    currentTime.value = now
    if (!force && loaded.value && lastFetchedAt.value !== null && now - lastFetchedAt.value < CACHE_TTL_MS) {
      return activeSubscriptions.value
    }

    const generation = requestGeneration
    lastAttemptAt = now
    loading.value = true
    error.value = null
    const requestPromise = subscriptionsAPI.getActiveSubscriptions()
      .then((data) => {
        if (generation === requestGeneration) {
          activeSubscriptions.value = data
          loaded.value = true
          lastFetchedAt.value = Date.now()
          currentTime.value = Date.now()
          error.value = null
          scheduleExpiryRefresh()
        }
        return data
      })
      .catch((cause: unknown) => {
        if (generation === requestGeneration) error.value = 'Failed to fetch active subscriptions'
        throw cause
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })
    activePromise = requestPromise
    return requestPromise
  }

  function handleVisibilityChange() {
    if (document.visibilityState === 'hidden') return
    currentTime.value = Date.now()
    scheduleExpiryRefresh()
    // Visibility events cannot create an unbounded retry loop, including after errors.
    if (lastAttemptAt === null || Date.now() - lastAttemptAt >= CACHE_TTL_MS) {
      fetchActiveSubscriptions().catch(() => {})
    }
  }

  /** App.vue owns this singleton lifecycle; surfaces never create their own timers. */
  function startPolling() {
    if (pollerInterval) return
    currentTime.value = Date.now()
    scheduleExpiryRefresh()
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', handleVisibilityChange)
    pollerInterval = setInterval(() => {
      currentTime.value = Date.now()
      if (typeof document === 'undefined' || document.visibilityState !== 'hidden') {
        fetchActiveSubscriptions(true).catch(() => {})
      }
    }, POLL_INTERVAL_MS)
  }

  function stopPolling() {
    if (pollerInterval) clearInterval(pollerInterval)
    if (expiryTimeout) clearTimeout(expiryTimeout)
    pollerInterval = null
    expiryTimeout = null
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', handleVisibilityChange)
  }

  function clear() {
    requestGeneration++
    activePromise = null
    loading.value = false
    activeSubscriptions.value = []
    loaded.value = false
    error.value = null
    lastFetchedAt.value = null
    lastAttemptAt = null
    currentTime.value = Date.now()
    stopPolling()
  }

  function invalidateCache() {
    lastFetchedAt.value = null
  }

  onScopeDispose(stopPolling)

  return {
    activeSubscriptions,
    currentSubscriptions,
    allowances,
    loading,
    loaded,
    error,
    lastFetchedAt,
    currentTime,
    hasActiveSubscriptions,
    fetchActiveSubscriptions,
    startPolling,
    stopPolling,
    clear,
    invalidateCache
  }
})
