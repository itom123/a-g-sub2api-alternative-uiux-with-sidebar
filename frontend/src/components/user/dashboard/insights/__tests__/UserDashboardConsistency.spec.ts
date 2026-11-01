import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardConsistency from '../UserDashboardConsistency.vue'
import type { DashboardConsistency } from '../types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const summary: DashboardConsistency = { activeDays30: 5, callsPerActiveDay: 2.5, tokensPerCall: 120, trend7d: 'up', recentCalls: 10, priorCalls: 8 }

describe('UserDashboardConsistency', () => {
  it('shows self-only consistency metrics and trend', () => {
    const wrapper = mount(UserDashboardConsistency, { props: { summary, loading: false }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.text()).toContain('5')
    expect(wrapper.text()).toContain('3')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('accountInsights.consistency.up')
    expect(wrapper.text()).not.toMatch(/team|peer|other user/i)
  })

  it('distinguishes loading and no-data states', async () => {
    const wrapper = mount(UserDashboardConsistency, { props: { summary: null, loading: true }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    await wrapper.setProps({ loading: false })
    expect(wrapper.text()).toContain('accountInsights.consistency.noData')
  })
})
