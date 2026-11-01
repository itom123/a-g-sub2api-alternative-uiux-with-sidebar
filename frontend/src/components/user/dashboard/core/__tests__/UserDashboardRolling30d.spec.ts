import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { currencyRate } from '@/utils/currency'
import UserDashboardRolling30d from '../UserDashboardRolling30d.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => { currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' } })
afterEach(() => { currencyRate.value = null })
describe('UserDashboardRolling30d', () => {
  it('renders independent rolling summary values', () => { const w = mount(UserDashboardRolling30d, { props: { loading: false, summary: { startDate: '2026-10-03', endDate: '2026-11-01', actualCostUsd: 2, requests: 10, totalTokens: 100, activeDays: 5, tokensPerCall: 10 } } }); expect(w.get('[data-testid=rolling-30d]').text()).toContain('accountInsights.rolling.title'); expect(w.text()).toContain('10'); expect(w.text()).toContain('100') })
  it('shows zero values without inventing data', () => { const w = mount(UserDashboardRolling30d, { props: { loading: false, summary: { startDate: '2026-10-03', endDate: '2026-11-01', actualCostUsd: 0, requests: 0, totalTokens: 0, activeDays: 0, tokensPerCall: 0 } } }); expect(w.text()).toContain('0') })
  it('renders missing spend as an em dash and uses two CNY decimals', () => { const missing = mount(UserDashboardRolling30d, { props: { loading: false, summary: { startDate: '2026-10-03', endDate: '2026-11-01', actualCostUsd: null, requests: null, totalTokens: null, activeDays: null, tokensPerCall: null } } }); expect(missing.text()).toContain('—'); expect(missing.text()).not.toContain('¥0.0000'); const present = mount(UserDashboardRolling30d, { props: { loading: false, summary: { startDate: '2026-10-03', endDate: '2026-11-01', actualCostUsd: 2, requests: null, totalTokens: null, activeDays: null, tokensPerCall: null } } }); expect(present.text()).toContain('¥14.00') })
})
