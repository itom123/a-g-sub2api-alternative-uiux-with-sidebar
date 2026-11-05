import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { SubscriptionQuotaWindow } from '@/utils/subscriptionQuota'
import { currencyRate } from '@/utils/currency'
import SubscriptionUsageCell from '../SubscriptionUsageCell.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string) => {
      const messages: Record<string, string> = {
        'common.notAvailable': 'N/A',
        'admin.subscriptions.unlimited': 'Unlimited'
      }
      return messages[key] ?? key
    }
  })
}))

function quota(overrides: Partial<SubscriptionQuotaWindow> = {}): SubscriptionQuotaWindow {
  return {
    period: 'daily', oneTime: false, state: 'finite', used: 0, limit: 10,
    remaining: 10, usagePercentage: 0,
    ...overrides
  }
}

afterEach(() => { currencyRate.value = null })

describe('SubscriptionUsageCell', () => {
  it('uses the same numeric and reset subline structure for all three periods', () => {
    currencyRate.value = null
    for (const period of ['daily', 'weekly', 'monthly'] as const) {
      const wrapper = mount(SubscriptionUsageCell, {
        props: { window: quota({ period }), resetText: 'Resets in 12h 0m' }
      })
      expect(wrapper.attributes('data-period')).toBe(period)
      expect(wrapper.attributes('data-state')).toBe('finite')
      expect(wrapper.get('[data-test="usage-used"]').text()).toBe('$0.00')
      expect(wrapper.get('[data-test="usage-limit"]').text()).toBe('$10.00')
      expect(wrapper.get('.subscription-usage-cell__reset').text()).toBe('Resets in 12h 0m')
      expect(wrapper.text()).not.toMatch(/Daily|Weekly|Monthly/)
      wrapper.unmount()
    }
  })

  it('keeps the subline container but omits text when no reset is supplied', () => {
    const wrapper = mount(SubscriptionUsageCell, { props: { window: quota() } })
    expect(wrapper.find('.subscription-usage-cell__window').exists()).toBe(true)
    expect(wrapper.get('.subscription-usage-cell__window').text()).toBe('')
    expect(wrapper.find('.subscription-usage-cell__reset').exists()).toBe(false)
    wrapper.unmount()
  })

  it('distinguishes uncapped allowance from unavailable data and never invents zero', async () => {
    currencyRate.value = null
    const wrapper = mount(SubscriptionUsageCell, {
      props: { window: quota({ state: 'unlimited', limit: null }) }
    })
    expect(wrapper.get('[data-test="usage-used"]').text()).toBe('$0.00')
    expect(wrapper.get('[data-test="usage-limit"]').text()).toBe('Unlimited')
    await wrapper.setProps({ window: quota({ state: 'unknown', used: null, limit: null }) })
    expect(wrapper.get('[data-test="usage-used"]').text()).toBe('N/A')
    expect(wrapper.get('[data-test="usage-limit"]').text()).toBe('N/A')
    expect(wrapper.text()).not.toContain('Unlimited')
    wrapper.unmount()
  })
})
