import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardKeyDrilldown from '../UserDashboardKeyDrilldown.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/currency', () => ({ cnyNumber: (value: number) => String(value) }))

const daily = { items: [{ date: '2026-10-31', requests: 3, input_tokens: 10, output_tokens: 20, cache_read_tokens: 2, cache_write_tokens: 1, total_tokens: 33, cost: 2, actual_cost: 1 }], days: 1, start_date: '2026-10-31', end_date: '2026-10-31' }

describe('UserDashboardKeyDrilldown', () => {
  it('renders daily token composition and model split', () => {
    const wrapper = mount(UserDashboardKeyDrilldown, { props: { keyInfo: { id: 1, name: 'my-key' } as never, daily, models: [{ model: 'model-a', requests: 3, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 1, cache_read_tokens: 2, total_tokens: 33, cost: 2, actual_cost: 1 }], loading: false } })
    expect(wrapper.text()).toContain('my-key')
    expect(wrapper.text()).toContain('33')
    expect(wrapper.text()).toContain('model-a')
    expect(wrapper.text()).not.toMatch(/secret|sk-/i)
    expect(wrapper.find('[data-testid="key-drilldown-panels"]').classes()).toContain('lg:grid-cols-1')
    expect(wrapper.find('[data-testid="key-daily-usage"]').classes()).toContain('min-w-0')
    expect(wrapper.find('[data-testid="key-daily-usage"] .md\\:block table').classes()).toContain('table-fixed')
    expect(wrapper.find('[data-testid="key-daily-usage"] .md\\:block table').classes()).not.toContain('min-w-[640px]')
    expect(wrapper.find('[data-testid="key-daily-usage"] .md\\:hidden').text()).toContain('accountInsights.charts.actual')
    expect(wrapper.find('[data-testid="key-daily-usage"] .md\\:hidden').text()).toContain('1')
    expect(wrapper.find('[data-testid="key-model-split"] .grid').classes()).toContain('min-w-0')
  })

  it('emits close and handles loading and empty detail states', async () => {
    const wrapper = mount(UserDashboardKeyDrilldown, { props: { keyInfo: null, daily: null, models: [], loading: false }, global: { stubs: { LoadingSpinner: true } } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.text()).toContain('accountInsights.empty.noUsage')
    expect(wrapper.text()).toContain('accountInsights.common.noData')
    await wrapper.setProps({ dailyError: true, modelsError: true })
    expect(wrapper.text()).toContain('accountInsights.drilldown.unavailable')
    await wrapper.setProps({ loading: true, dailyError: false, modelsError: false })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
  })
})
