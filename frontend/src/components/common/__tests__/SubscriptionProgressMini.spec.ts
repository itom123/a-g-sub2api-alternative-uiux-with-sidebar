import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Group, UserSubscription } from '@/types'
import { currencyRate } from '@/utils/currency'
import { useSubscriptionStore } from '@/stores/subscriptions'
import en from '@/i18n/locales/en/misc'
import zh from '@/i18n/locales/zh/misc'
import SubscriptionProgressMini from '../SubscriptionProgressMini.vue'

const api = vi.hoisted(() => ({ active: vi.fn(), enabled: true }))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: api.active } }))
vi.mock('@/stores', async () => ({ useSubscriptionStore: (await import('@/stores/subscriptions')).useSubscriptionStore }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription' }, isFeatureFlagEnabled: () => api.enabled }))

enableAutoUnmount(afterEach)
function plan(over: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 1, user_id: 42, group_id: 7, status: 'active', starts_at: '2026-09-01T00:00:00Z', expires_at: null,
    daily_usage_usd: 10, weekly_usage_usd: 70, monthly_usage_usd: 20,
    daily_window_start: null, weekly_window_start: null, monthly_window_start: null, created_at: '', updated_at: '',
    group: { name: 'Studio plan', daily_limit_usd: 10, weekly_limit_usd: 100, monthly_limit_usd: 25 } as Group,
    ...over
  }
}
function seed(plans = [plan()]) {
  api.active.mockResolvedValue(plans)
  const store = useSubscriptionStore()
  store.activeSubscriptions = plans
  store.loaded = true
  store.lastFetchedAt = Date.now()
  return store
}
function render(locale = 'en') {
  return mount(SubscriptionProgressMini, { global: {
    plugins: [createI18n({ legacy: false, locale, messages: { en, zh } })],
    stubs: { RouterLink: { template: '<a href="/subscriptions"><slot /></a>' } }
  } })
}
beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 22, 12))
  api.enabled = true
  api.active.mockReset()
  currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
})
afterEach(() => {
  useSubscriptionStore().clear()
  currencyRate.value = null
  vi.useRealTimers()
})

describe('subscription header allowance', () => {
  it.each(['en', 'zh'])('names the selected plan, period and real zero in %s', (locale) => {
    seed()
    const wrapper = render(locale)
    expect(wrapper.text()).toContain('Studio plan')
    expect(wrapper.get('[data-testid="subscription-mini-remaining"]').text()).toContain('¥0.00')
    expect(wrapper.text()).toContain(locale === 'en' ? 'Daily' : '每日')
    expect(wrapper.get('button').attributes('aria-label')).toContain('Studio plan')
  })

  it('uses stable urgency order and exposes every plan and period without pooling', async () => {
    seed([plan({ id: 9 }), plan({ id: 2, group: { name: 'Other plan', daily_limit_usd: 10, weekly_limit_usd: 100, monthly_limit_usd: 25 } as Group })])
    const wrapper = render()
    expect(wrapper.text()).toContain('Other plan')
    expect(wrapper.text()).toContain('+1 plans')
    await wrapper.get('button').trigger('click')
    expect(wrapper.findAll('[data-plan-id]').map((node) => node.attributes('data-plan-id'))).toEqual(['2', '9'])
    expect(wrapper.get('[role="dialog"]').text()).toContain('Weekly')
    expect(wrapper.get('[role="dialog"]').text()).toContain('Monthly')
    expect(wrapper.get('[role="dialog"]').text()).toContain('¥210.00')
    expect(wrapper.get('[role="dialog"]').text()).toContain('¥35.00')
  })

  it('distinguishes unlimited from missing group data', async () => {
    seed([plan({ group: { name: 'Uncapped', daily_limit_usd: null, weekly_limit_usd: 0, monthly_limit_usd: -1 } as Group })])
    const wrapper = render()
    expect(wrapper.text()).toContain('Unlimited')
    wrapper.unmount()
    seed([plan({ group: undefined })])
    const unknown = render()
    expect(unknown.text()).toContain('Unknown')
    expect(unknown.text()).not.toContain('Unlimited')
    await unknown.get('button').trigger('click')
    expect(unknown.get('[role="dialog"]').text()).toContain('Unknown')
  })

  it('opens by click and closes on Escape, outside click, and link selection', async () => {
    seed()
    const wrapper = render()
    const button = wrapper.get('button')
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await button.trigger('click')
    document.body.click()
    await wrapper.vm.$nextTick()
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    await wrapper.get('a').trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
  })

  it('shows initial loading or failure rather than claiming no subscription', async () => {
    api.active.mockRejectedValueOnce(new Error('offline'))
    const wrapper = render()
    expect(wrapper.text()).toContain('Loading subscriptions')
    await flushPromises()
    expect(wrapper.text()).toContain('Unknown')
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('[role="status"]').text()).toContain('could not be loaded')
  })

  it('retains an old allowance and flags a refresh error', async () => {
    const store = seed()
    api.active.mockRejectedValueOnce(new Error('offline'))
    await store.fetchActiveSubscriptions(true).catch(() => {})
    const wrapper = render()
    expect(wrapper.text()).toContain('¥0.00')
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('[role="status"]').text()).toContain('last successful snapshot')
  })

  it('hides a confirmed empty snapshot and obeys the subscription flag without fetching', () => {
    seed([])
    expect(render().find('[data-testid="subscription-mini"]').exists()).toBe(false)
    api.enabled = false
    seed()
    api.active.mockClear()
    expect(render().find('[data-testid="subscription-mini"]').exists()).toBe(false)
    expect(api.active).not.toHaveBeenCalled()
  })

  it('uses USD with two decimals when the currency rate is unavailable', () => {
    currencyRate.value = null
    seed([plan({ daily_usage_usd: 9.96 })])
    expect(render().text()).toContain('$0.04')
  })

  it.each([
    [new Date(2026, 8, 22, 18), 'Expires today'],
    [new Date(2026, 8, 23, 18), 'Expires tomorrow'],
    [new Date(2026, 8, 25, 12), '3 days left']
  ])('uses the existing calendar expiry label for %s', async (expires, label) => {
    seed([plan({ expires_at: expires.toISOString() })])
    const wrapper = render()
    await wrapper.get('button').trigger('click')
    expect(wrapper.text()).toContain(label)
  })

  it('does not display expired or revoked plans as current allowances', () => {
    seed([plan({ expires_at: new Date(2026, 8, 22, 12).toISOString() }), plan({ id: 2, status: 'revoked' })])
    expect(render().find('[data-testid="subscription-mini"]').exists()).toBe(false)
  })
})
