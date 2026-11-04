import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import UserDashboardCharts from '../../UserDashboardCharts.vue'
import type { DashboardRange } from '../types'
import { currencyRate } from '@/utils/currency'
import { accountGroupContrastPlugin } from '../groupContrastPlugin'

vi.mock('vue-chartjs', () => ({ Line: { name: 'Line', props: ['data', 'options', 'plugins'], template: '<canvas />' } }))

vi.mock('vue-i18n', async () => { const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n'); return { ...actual, useI18n: () => ({ t: (key: string) => key }) } })

const range: DashboardRange = { startDate: '2026-10-01', endDate: '2026-10-31', timezone: 'UTC', preset: '30d' }
const capture = (name: string, propNames: string[]) => defineComponent({
  props: Object.fromEntries(propNames.map((prop) => [prop, { type: null, required: false }])) as Record<string, unknown>,
  setup(props) {
    return () => h('div', { 'data-testid': name, 'data-props': JSON.stringify(props) })
  },
})

describe('UserDashboardCharts', () => {
  it('passes display-only chart flags and uses actual-cost spend data', () => {
    const wrapper = mount(UserDashboardCharts, {
      props: {
        loading: false,
        range,
        trend: [{ date: '2026-10-01', requests: 2, input_tokens: 3, output_tokens: 4, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 7, cost: 9, actual_cost: 1.5 }],
        models: [{ model: 'm', requests: 1, input_tokens: 1, output_tokens: 1, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 2, cost: 2, actual_cost: 1 }],
        groups: [{ group_id: 1, group_name: 'g', requests: 1, total_tokens: 2, cost: 2, actual_cost: 1 }],
        rangeStats: null,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
          Line: true,
          TokenUsageTrend: capture('token-trend', ['trendData', 'loading', 'embedded']),
          ModelDistributionChart: capture('model-chart', ['modelStats', 'loading', 'enableBreakdown', 'enableRankingView', 'showAccountCost']),
          GroupDistributionChart: capture('group-chart', ['groupStats', 'loading', 'enableBreakdown', 'showAccountCost']),
        },
      },
    })
    expect(wrapper.find('[data-testid="model-chart"]').attributes('data-props')).toContain('"enableBreakdown":false')
    expect(wrapper.find('[data-testid="model-chart"]').attributes('data-props')).toContain('"enableRankingView":false')
    expect(wrapper.find('[data-testid="model-chart"]').attributes('data-props')).toContain('"showAccountCost":false')
    expect(wrapper.find('[data-testid="group-chart"]').attributes('data-props')).toContain('"enableBreakdown":false')
    expect(wrapper.find('[data-testid="group-chart"]').attributes('data-props')).toContain('"showAccountCost":false')
    expect(wrapper.text()).toContain('accountInsights.charts.spendTrend')
    const trends = wrapper.get('[data-testid="account-trends"]')
    expect(trends.find('[data-testid="account-spending-trend"]').exists()).toBe(true)
    expect(trends.get('[data-testid="token-trend"]').attributes('data-props')).toContain('"embedded":true')
    expect(trends.find('[data-testid="model-chart"]').exists()).toBe(false)
    expect(trends.element.nextElementSibling?.getAttribute('data-testid')).toBe('model-chart')
    wrapper.unmount()
  })

  it('assigns explicit dark-mode fills and borders to every group arc', () => {
    document.documentElement.classList.add('dark')
    const dataset = { data: [10, 20, 30], backgroundColor: ['#000'], borderWidth: 0 } as any
    const canvas = document.createElement('canvas')
    const wrapper = document.createElement('div')
    wrapper.className = 'account-group-distribution'
    wrapper.appendChild(canvas)
    document.body.appendChild(wrapper)
    const chart = { canvas, data: { datasets: [dataset] }, options: {} } as any
    const contextSpy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    accountGroupContrastPlugin.beforeDatasetUpdate?.(chart, {} as any, {} as any)
    contextSpy.mockRestore()
    expect(dataset.backgroundColor).toHaveLength(3)
    expect(dataset.backgroundColor[0]).toBe('#000000')
    expect(dataset.borderColor).toEqual(['#000000', '#000000', '#000000'])
    expect(dataset.borderWidth).toBe(2)
    wrapper.remove()
    document.documentElement.classList.remove('dark')
  })

  it('uses USD fallback formatting when currency is unavailable', () => {
    currencyRate.value = null
    const wrapper = mount(UserDashboardCharts, {
      props: { loading: false, range, trend: [{ date: '2026-10-01', requests: 1, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0, cost: 0, actual_cost: 1.2345 }], models: [], groups: [], rangeStats: { total_actual_cost: 1.2345 } as never },
      global: { stubs: { LoadingSpinner: true, Line: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true } },
    })
    expect(wrapper.text()).toContain('$1.2345')
    wrapper.unmount()
  })

  it('preserves a true zero actual total and falls back to trend only for missing totals', async () => {
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    const wrapper = mount(UserDashboardCharts, {
      props: { loading: false, range, trend: [{ date: '2026-10-01', requests: 1, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 0, cost: 9, actual_cost: 1.5 }], models: [], groups: [], rangeStats: { total_actual_cost: 0 } as never },
      global: { stubs: { LoadingSpinner: true, Line: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true } },
    })
    expect(wrapper.get('[data-testid="account-spending-trend"]').text()).toContain('¥0.00')
    await wrapper.setProps({ rangeStats: { total_actual_cost: null } as never })
    expect(wrapper.get('[data-testid="account-spending-trend"]').text()).toContain('¥10.50')
    wrapper.unmount()
    currencyRate.value = null
  })

  it('keeps cost data in USD and formats it exactly once in reactive currency options', async () => {
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    const wrapper = mount(UserDashboardCharts, {
      props: { loading: false, range, trend: [{ date: '2026-10-01', requests: 1, input_tokens: 1, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 1, cost: 9, actual_cost: 1.5 }], models: [], groups: [], rangeStats: null },
      global: { stubs: { LoadingSpinner: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true } },
    })
    const line = wrapper.findComponent({ name: 'Line' })
    expect((line.props('data') as any).datasets[0].data).toEqual([1.5])
    const options = line.props('options') as any
    expect(options.plugins.tooltip.callbacks.label({ dataset: { label: 'Actual' }, raw: 1.5 })).toBe('Actual: ¥10.50')
    expect(options.scales.y.title.text).toBe('CNY')
    currencyRate.value = null
    await wrapper.vm.$nextTick()
    expect((line.props('options') as any).scales.y.title.text).toBe('USD')
    await wrapper.get('[data-testid="account-trends"]').trigger('keydown', { key: 'Home' })
    expect(wrapper.get('[data-testid="account-trend-readout"]').text()).toContain('2026-10-01')
    expect(wrapper.get('[data-testid="account-trend-readout"]').text()).toContain('$1.5000')
    await wrapper.get('[data-testid="account-trends"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.get('[data-testid="account-trend-readout"]').text()).toContain('accountInsights.charts.noSelection')
    wrapper.unmount()
  })

  it('renders loading and empty states without chart data', async () => {
    const wrapper = mount(UserDashboardCharts, {
      props: { loading: true, range, trend: [], models: [], groups: [], rangeStats: null },
      global: { stubs: { LoadingSpinner: true, Line: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true } },
    })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    await wrapper.setProps({ loading: false })
    expect(wrapper.text()).toContain('accountInsights.charts.noData')
    wrapper.unmount()
  })
})
