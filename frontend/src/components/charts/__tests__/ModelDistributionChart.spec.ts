import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelDistributionChart from '../ModelDistributionChart.vue'

const messages: Record<string, string> = {
  'admin.dashboard.modelDistribution': 'Model Distribution',
  'admin.dashboard.spendingRankingTitle': 'User Spending Ranking',
  'admin.dashboard.viewModelDistribution': 'Model Distribution',
  'admin.dashboard.viewSpendingRanking': 'User Spending Ranking',
  'admin.dashboard.spendingRankingUser': 'User',
  'admin.dashboard.spendingRankingRequests': 'Requests',
  'admin.dashboard.spendingRankingTokens': 'Tokens',
  'admin.dashboard.spendingRankingSpend': 'Spend',
  'admin.dashboard.spendingRankingOther': 'Others',
  'admin.dashboard.model': 'Model',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
  'admin.redeem.userPrefix': 'User #{id}',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const message = messages[key] ?? key
        return params
          ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name]))
          : message
      },
    }),
  }
})

const getUserBreakdown = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown }))

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('ModelDistributionChart', () => {
  const modelStats = [
    {
      model: 'model-a',
      requests: 8,
      input_tokens: 100,
      output_tokens: 50,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 1000,
      cost: 1.5,
      actual_cost: 0.2,
    },
    {
      model: 'model-b',
      requests: 3,
      input_tokens: 40,
      output_tokens: 20,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 500,
      cost: 0.5,
      actual_cost: 1.4,
    },
  ]

  it('shows full-width shares including tiny models and recalculates them for spending', async () => {
    const wrapper = mount(ModelDistributionChart, {
      props: { modelStats: [...modelStats, { ...modelStats[0], model: 'tiny-model-with-a-complete-name', total_tokens: 1, actual_cost: 0 }], fullWidth: true, enableBreakdown: false },
      global: { stubs: { LoadingSpinner: true } }
    })
    expect(wrapper.find('.chart-data').exists()).toBe(false)
    expect(wrapper.findAll('[role="img"]').map(e => e.attributes('aria-label'))).toEqual(['model-a: 66.6%', 'model-b: 33.3%', 'tiny-model-with-a-complete-name: <0.1%'])
    await wrapper.setProps({ metric: 'actual_cost' })
    expect(wrapper.findAll('[role="img"]').map(e => e.attributes('aria-label'))).toEqual(['model-b: 87.5%', 'model-a: 12.5%', 'tiny-model-with-a-complete-name: 0.0%'])
    await wrapper.setProps({ modelStats: modelStats.map(m => ({ ...m, actual_cost: 0 })) })
    expect(wrapper.findAll('[role="img"]').every(e => e.attributes('aria-label')?.endsWith('0.0%'))).toBe(true)
  })

  it('places user breakdown rows in the parent table columns', async () => {
    getUserBreakdown.mockResolvedValue({
      users: [{ user_id: 7, email: 'user@example.com', requests: 5, total_tokens: 600, cost: 1, actual_cost: 0.1, account_cost: 0.9 }],
    })
    const wrapper = mount(ModelDistributionChart, {
      props: { modelStats, fullWidth: true },
      global: { stubs: { LoadingSpinner: true } }
    })
    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    const breakdown = wrapper.find('tbody > tr.ag-breakdown-row')
    expect(breakdown.find('table').exists()).toBe(false)
    const cells = breakdown.findAll('td')
    expect(cells[0].text()).toBe('user@example.com')
    // The label covers the model and share columns; each metric keeps its own column.
    expect(cells[0].attributes('colspan')).toBe('2')
    expect(cells.length + 1).toBe(wrapper.findAll('thead th').length)
  })

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.find('.chart-data').exists()).toBe(false)
    expect(wrapper.findAll('.ag-distribution-mini [role=img]').map(x => x.attributes('aria-label'))).toEqual(['model-a: 66.7%', 'model-b: 33.3%'])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-a')
    expect(rows[1].text()).toContain('model-b')

    expect(rows[0].text()).toContain('1.00K')
  })

  it('uses actual_cost and reorders rows in actual cost mode', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        metric: 'actual_cost',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.findAll('.ag-distribution-mini [role=img]').map(x => x.attributes('aria-label'))).toEqual(['model-b: 87.5%', 'model-a: 12.5%'])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-b')
    expect(rows[1].text()).toContain('model-a')

    expect(rows[0].text()).toContain('¥1.40')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        showAccountCost: false,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Account Cost')
    expect(wrapper.findAll('thead th')).toHaveLength(5)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(5)
  })

  it('uses the dashboard user label policy and renders Others with a dedicated chart color', async () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats: [],
        enableRankingView: true,
        rankingItems: [
          { user_id: 1, email: 'alpha@example.com', username: 'alpha', actual_cost: 12, requests: 10, tokens: 1000 },
          { user_id: 2, email: 'beta@example.com', username: '   ', actual_cost: 8, requests: 6, tokens: 600 },
          { user_id: 3, email: '   ', username: '', actual_cost: 0, requests: 0, tokens: 0 },
        ],
        rankingTotalActualCost: 30,
        rankingTotalRequests: 20,
        rankingTotalTokens: 2000,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const rankingButton = wrapper.findAll('button').find((button) => button.text() === 'User Spending Ranking')
    expect(rankingButton).toBeTruthy()
    await rankingButton!.trigger('click')

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual([
      '#1 alpha',
      '#2 beta@example.com',
      '#3 User #3',
      'Others',
    ])
    expect(chartData.datasets[0].data).toEqual([12, 8, 0, 10])
    expect(chartData.datasets[0].backgroundColor[0]).toBe('#3b82f6')
    expect(chartData.datasets[0].backgroundColor[3]).toBe('#94a3b8')
    expect(chartData.datasets[0].backgroundColor[3]).not.toBe(chartData.datasets[0].backgroundColor[0])

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('alpha')
    expect(rows[0].text()).not.toContain('alpha@example.com')
    expect(rows[1].text()).toContain('beta@example.com')
    expect(rows[2].text()).toContain('User #3')
    expect(rows[3].text()).toContain('Others')
    expect(rows[3].text()).toContain('4')
    expect(rows[3].text()).toContain('400')
    expect(rows[3].text()).toContain('¥10.00')
  })
})
