import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardKeyRanking from '../UserDashboardKeyRanking.vue'
import type { DashboardKeyUsageRow } from '../types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const rows: DashboardKeyUsageRow[] = [
  { apiKeyId: 2, keyName: 'busy-key', status: 'active', selectedRangeCostUsd: 2, lifetimeCostUsd: 4, todayCostUsd: 1, requests: 10, inputTokens: 1, cacheCreationTokens: 0, cacheReadTokens: 0, outputTokens: 2, totalTokens: 3, favouriteModel: 'model-a', lastUsedAt: '2026-10-31T00:00:00Z', truncated: false },
  { apiKeyId: 1, keyName: 'empty-key', status: 'active', selectedRangeCostUsd: null, lifetimeCostUsd: null, todayCostUsd: null, requests: null, inputTokens: null, cacheCreationTokens: null, cacheReadTokens: null, outputTokens: null, totalTokens: null, favouriteModel: null, lastUsedAt: null, truncated: false },
]

describe('UserDashboardKeyRanking', () => {
  it('renders the already sorted input with missing values last and emits only requested sort changes', async () => {
    const wrapper = mount(UserDashboardKeyRanking, { props: { rows, loading: false, selectedKeyId: null, truncated: false }, global: { stubs: { LoadingSpinner: true } } })
    const rendered = wrapper.findAll('tbody tr')
    expect(rendered[0].text()).toContain('busy-key')
    expect(rendered[1].text()).toContain('empty-key')
    expect(wrapper.text()).not.toContain('sk-')
    expect(wrapper.text()).not.toContain('secret')
    await wrapper.findAll('thead button')[0].trigger('click')
    expect(wrapper.emitted('sort-change')).toEqual([[ 'cost' ]])
    expect(wrapper.emitted('select-key')).toBeUndefined()
  })

  it('emits key selection without exposing secret material', async () => {
    const wrapper = mount(UserDashboardKeyRanking, { props: { rows, loading: false, selectedKeyId: null, truncated: true }, global: { stubs: { LoadingSpinner: true } } })
    await wrapper.findAll('tbody button')[0].trigger('click')
    expect(wrapper.emitted('select-key')).toEqual([[2]])
    expect(wrapper.text()).toContain('accountInsights.keys.rowsCapped')
  })

  it('uses one shared fixed column structure on desktop and stacked mobile cells', () => {
    const wrapper = mount(UserDashboardKeyRanking, { props: { rows, loading: false, selectedKeyId: null, truncated: false }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.find('[data-testid="account-key-ranking"]').classes()).toContain('xl:col-span-2')
    expect(wrapper.find('table').classes()).toContain('block')
    expect(wrapper.find('table').classes()).toContain('md:table')
    expect(wrapper.find('table').classes()).toContain('md:table-fixed')
    expect(wrapper.find('colgroup').classes()).toContain('hidden')
    expect(wrapper.find('colgroup').classes()).toContain('md:table-column-group')
    expect(wrapper.findAll('col')).toHaveLength(9)
    expect(wrapper.find('thead').classes()).toContain('hidden')
    expect(wrapper.find('tbody tr td[data-label="accountInsights.keys.favouriteModel"]').classes()).toContain('grid')
    expect(wrapper.find('tbody tr td[data-label="accountInsights.keys.lastUse"]').classes()).toContain('grid')
    expect(wrapper.find('tbody tr').classes()).toContain('md:table-row')
    expect(wrapper.find('tbody tr td[data-label="accountInsights.keys.favouriteModel"]').classes()).toContain('grid-cols-[7rem_minmax(0,1fr)]')
    expect(wrapper.find('tbody tr td[data-label="accountInsights.keys.favouriteModel"] span').classes()).toContain('break-words')
    expect(wrapper.find('tbody tr button').classes()).toContain('whitespace-normal')
  })
  it('uses an em dash for missing call and token values', () => {
    const wrapper = mount(UserDashboardKeyRanking, { props: { rows, loading: false, selectedKeyId: null, truncated: false }, global: { stubs: { LoadingSpinner: true } } })
    const emptyRow = wrapper.findAll('tbody tr')[1]
    expect(emptyRow.findAll('td')[4].text()).toContain('—')
    expect(emptyRow.findAll('td')[5].text()).toContain('—')
  })

})
