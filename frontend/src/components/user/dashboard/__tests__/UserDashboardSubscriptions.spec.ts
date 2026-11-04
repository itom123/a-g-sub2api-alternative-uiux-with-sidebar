import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Group, UserSubscription } from '@/types'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { currencyRate } from '@/utils/currency'
import en from '@/i18n/locales/en/misc'
import zh from '@/i18n/locales/zh/misc'
import UserDashboardSubscriptions from '../UserDashboardSubscriptions.vue'

const api = vi.hoisted(() => ({ active: vi.fn(), enabled: true }))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: api.active } }))
vi.mock('@/stores', async () => ({ useSubscriptionStore: (await import('@/stores/subscriptions')).useSubscriptionStore }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription' }, isFeatureFlagEnabled: () => api.enabled }))
enableAutoUnmount(afterEach)
const plan = (over: Partial<UserSubscription> = {}): UserSubscription => ({
  id: 1, user_id: 42, group_id: 7, status: 'active', starts_at: '2026-11-01T00:00:00Z', expires_at: '2026-12-01T00:00:00Z',
  daily_usage_usd: 10, weekly_usage_usd: 70, monthly_usage_usd: 20,
  daily_window_start: null, weekly_window_start: null, monthly_window_start: null, created_at: '', updated_at: '',
  group: { name: 'Studio plan', daily_limit_usd: 10, weekly_limit_usd: 100, monthly_limit_usd: 25 } as Group, ...over
})
function seed(plans = [plan()]) {
  const store = useSubscriptionStore()
  store.activeSubscriptions = plans
  store.loaded = true
  store.lastFetchedAt = Date.now()
  api.active.mockResolvedValue(plans)
  return store
}
function render(locale = 'en') {
  return mount(UserDashboardSubscriptions, { global: { plugins: [createI18n({ legacy: false, locale, messages: { en, zh } })] } })
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-01T12:00:00Z'))
  setActivePinia(createPinia())
  api.enabled = true
  api.active.mockReset()
  currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
})
afterEach(() => { useSubscriptionStore().clear(); currencyRate.value = null; vi.useRealTimers() })

describe('UserDashboardSubscriptions', () => {
  it.each(['en', 'zh'])('renders every quota independently with usage/limits, zero and expiry in %s', (locale) => {
    seed()
    const wrapper = render(locale)
    expect(wrapper.findAll('[data-period]')).toHaveLength(3)
    expect(wrapper.get('[data-period="daily"]').text()).toContain('¥0.00')
    expect(wrapper.get('[data-period="weekly"]').text()).toContain('¥210.00')
    expect(wrapper.get('[data-period="weekly"]').text()).toContain('¥490.00')
    expect(wrapper.get('[data-period="weekly"]').text()).toContain('¥700.00')
    expect(wrapper.get('[data-period="monthly"]').text()).toContain('¥35.00')
    expect(wrapper.text()).toContain('2026')
    expect(wrapper.text()).toContain('Studio plan')
  })

  it('shows unknown separately from an explicitly uncapped plan', () => {
    seed([plan({ group: undefined }), plan({ id: 2, group: { name: 'Uncapped', daily_limit_usd: null, weekly_limit_usd: 0, monthly_limit_usd: -1 } as Group })])
    const wrapper = render()
    expect(wrapper.get('[data-plan-id="1"]').text()).toContain('Unknown')
    expect(wrapper.get('[data-plan-id="1"]').text()).not.toContain('Unlimited')
    expect(wrapper.get('[data-plan-id="2"]').text()).toContain('Unlimited')
  })

  it('shows loading, local error, and a working retry without clearing unrelated state', async () => {
    api.active.mockRejectedValueOnce(new Error('offline'))
    const wrapper = render()
    expect(wrapper.text()).toContain('Loading subscriptions')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('could not be loaded')
    api.active.mockResolvedValueOnce([plan()])
    await wrapper.get('[role="status"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Studio plan')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('keeps last-good allowances visible when refreshing fails', async () => {
    const store = seed()
    api.active.mockRejectedValueOnce(new Error('offline'))
    await store.fetchActiveSubscriptions(true).catch(() => {})
    const wrapper = render()
    expect(wrapper.get('[data-testid="subscription-refresh-error"]').text()).toContain('last successful snapshot')
    expect(wrapper.get('[data-period="daily"]').text()).toContain('¥0.00')
    expect(wrapper.text()).toContain('Updated')
  })

  it('does not render a confirmed empty or expired snapshot', () => {
    seed([])
    expect(render().find('[data-testid="dashboard-subscriptions"]').exists()).toBe(false)
    seed([plan({ expires_at: '2026-11-01T12:00:00Z' })])
    expect(render().find('[data-testid="dashboard-subscriptions"]').exists()).toBe(false)
  })

  it('obeys the disabled feature without fetching even when subscriptions exist', () => {
    seed()
    api.enabled = false
    api.active.mockClear()
    expect(render().find('[data-testid="dashboard-subscriptions"]').exists()).toBe(false)
    expect(api.active).not.toHaveBeenCalled()
  })

  it('uses the one-time daily label and does not invent reset timestamps', () => {
    seed([plan({ expires_at: '2026-11-02T00:00:00Z' })])
    const wrapper = render()
    expect(wrapper.get('[data-period="daily"]').text()).toContain('One-time')
    expect(wrapper.text().toLowerCase()).not.toContain('reset')
  })
})
