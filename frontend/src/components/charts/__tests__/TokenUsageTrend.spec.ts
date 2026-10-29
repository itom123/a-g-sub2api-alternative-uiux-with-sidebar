import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import TokenUsageTrend from '../TokenUsageTrend.vue'

const messages: Record<string, string> = {
  'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
  'admin.dashboard.tokenUsageTrendHint': 'All token series share one scale',
  'admin.dashboard.cacheReadAxis': 'Cache read tokens',
  'admin.dashboard.promptTokensAxis': 'Input / output tokens',
  'admin.dashboard.hitRateAxis': 'Hit rate',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    name: 'Line',
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('TokenUsageTrend', () => {
  it('calculates cache hit rate against all prompt tokens', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 500,
            output_tokens: 100,
            cache_creation_tokens: 0,
            cache_read_tokens: 1500,
            cost: 0.01,
            actual_cost: 0.005,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const options = wrapper.findComponent({ name: 'Line' }).props('options') as any
    const tooltip = options.plugins.tooltip.callbacks.footer([{ dataIndex: 0 }])
    // Hit rate = 1500 / (500 + 1500 + 0) * 100 = 75%
    expect(tooltip).toContain('Cache Hit Rate: 75.0%')
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 0,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            cost: 0,
            actual_cost: 0,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const options = wrapper.findComponent({ name: 'Line' }).props('options') as any
    const tooltip = options.plugins.tooltip.callbacks.footer([{ dataIndex: 0 }])
    expect(tooltip).toContain('Cache Hit Rate: 0.0%')
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const options = wrapper.findComponent({ name: 'Line' }).props('options') as any
    const tooltip = options.plugins.tooltip.callbacks.footer([{ dataIndex: 0 }])
    // Hit rate = 500 / (200 + 500 + 300) * 100 = 50%
    expect(tooltip).toContain('Cache Hit Rate: 50.0%')
  })

  it('plots all four token series against one zero-based left axis', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 5000,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.datasets).toHaveLength(4)
    expect(chartData.datasets.every((ds: any) => ds.yAxisID === 'y')).toBe(true)
    expect(chartData.datasets.map((ds: any) => ds.label)).toEqual(['Input', 'Output', 'Cache Creation', 'Cache Read'])
    const options = wrapper.findComponent({ name: 'Line' }).props('options') as any
    expect(Object.keys(options.scales)).toEqual(['x', 'y'])
    expect(options.scales.y.position).toBe('left')
    expect(options.scales.y.beginAtZero).toBe(true)

  })

  it('uses shared dashboard labels when provided', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        labels: ['2026-05-07', '2026-05-08'],
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 0,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['2026-05-07', '2026-05-08'])
  })
})
