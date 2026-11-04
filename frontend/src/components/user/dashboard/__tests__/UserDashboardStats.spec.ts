import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { currencyRate } from '@/utils/currency'
import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats } from '@/api/usage'
import type { PlatformQuotaItem, User, UsageStatsResponse } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))

const profile = (over: Partial<User> = {}): User => ({ id: 42, username: 'Account', email: 'account@example.invalid', role: 'user', balance: 20, frozen_balance: 2, concurrency: 2, status: 'active', allowed_groups: [], balance_notify_enabled: true, balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '', updated_at: '', ...over })
const stats = (over: Partial<UserDashboardStats> = {}): UserDashboardStats => ({ total_api_keys: 2, active_api_keys: 1, total_requests: 120, total_input_tokens: 20, total_output_tokens: 40, total_cache_creation_tokens: 5, total_cache_read_tokens: 10, total_tokens: 75, total_cost: 9, total_actual_cost: 10, today_requests: 8, today_input_tokens: 2, today_output_tokens: 3, today_cache_creation_tokens: 1, today_cache_read_tokens: 4, today_tokens: 10, today_cost: 1, today_actual_cost: 1.5, average_duration_ms: 123, rpm: 4, tpm: 90, by_platform: [], ...over })
const range = (over: Partial<UsageStatsResponse> = {}): UsageStatsResponse => ({ total_requests: 12, total_input_tokens: 20, total_output_tokens: 40, total_cache_tokens: 15, total_cache_read_tokens: 10, total_cache_creation_tokens: 5, total_tokens: 75, total_cost: 8, total_actual_cost: 9, average_duration_ms: 100, ...over })
const quota = (platform: string, over: Partial<PlatformQuotaItem> = {}): PlatformQuotaItem => ({ platform: platform as PlatformQuotaItem['platform'], daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null, daily_usage_usd: 0, weekly_usage_usd: 0, monthly_usage_usd: 0, ...over })
function render(over: Record<string, unknown> = {}) { return mount(UserDashboardStats, { props: { profile: profile(), dashboardStats: stats(), rangeStats: range(), rolling30: null, quotas: [], health: 'all_good', lowBalance: { visible: false, thresholdCny: 210, dismissed: false }, ...over }, global: { stubs: { Icon: true, UserDashboardBalanceWarning: true, UserDashboardRolling30d: true }, mocks: { $router: { push: vi.fn() } } } }) }

afterEach(() => { currencyRate.value = null })
beforeEach(() => { currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' } })

describe('UserDashboardStats', () => {
  it('renders an awaiting state instead of crashing without profile', () => { const w = mount(UserDashboardStats, { props: {}, global: { stubs: { UserDashboardHealthChip: true, UserDashboardBalanceWarning: true, UserDashboardRolling30d: true } } }); expect(w.get('[role=status]').text()).toContain('accountInsights.awaitingData') })
  it('renders cash and today spend without frozen details or a fabricated balance percentage', () => { const w = render(); expect(w.get('[data-testid="account-balance"]').text()).toContain('¥140.00'); expect(w.text()).toContain('¥63.00'); expect(w.text()).toContain('¥10.50') })
  it('renders an em dash for missing spend values', () => { const w = render({ rangeStats: range({ total_actual_cost: null }), dashboardStats: stats({ today_actual_cost: null }) }); expect(w.get('[data-testid="account-stat-grid"]').text()).toContain('—'); expect(w.get('[data-testid="account-stat-grid"]').text()).not.toContain('¥0.0000') })
  it('renders four token buckets and response metrics', () => { const w = render(); expect(w.get('[data-testid="token-composition"]').text()).toContain('20'); expect(w.get('[data-testid="token-composition"]').text()).toContain('5'); expect(w.get('[data-testid="token-composition"]').text()).toContain('10'); expect(w.get('[data-testid="token-composition"]').text()).toContain('40'); expect(w.get('[data-testid="account-stat-grid"]').text()).toContain('100'); expect(w.get('[data-testid="account-stat-grid"]').text()).toContain('4'); expect(w.get('[data-testid="account-stat-grid"]').text()).toContain('90') })
  it.each(['awaiting_data', 'all_good', 'attention'] as const)('replaces decorative health state %s with independent key-count boxes', (health) => {
    const w = render({ health })
    expect(w.get('[data-testid="active-keys-card"]').text()).toContain('1')
    expect(w.get('[data-testid="total-keys-card"]').text()).toContain('2')
    expect(w.find('[data-testid="health-card"]').exists()).toBe(false)
    expect(w.find('[data-testid="health-chip"]').exists()).toBe(false)
    expect(w.text()).not.toContain('accountInsights.scope')
    expect(w.get('[data-testid="balance-card"]').text()).not.toContain('✓')
    expect(w.get('[data-testid="account-stat-grid"]').text()).not.toContain('accountInsights.stats.activeKeys')
  })
  it('renders quota at 80%, 100%, disabled and unlimited with reset time', () => { const w = render({ quotas: [quota('openai', { daily_limit_usd: 10, daily_usage_usd: 8, daily_window_resets_at: '2026-11-02T00:00:00Z', weekly_limit_usd: 10, weekly_usage_usd: 10, monthly_limit_usd: 0 }), quota('anthropic', { weekly_limit_usd: 5 })] }); expect(w.get('[data-testid="quota-bars"]').text()).toContain('80%'); expect(w.get('[data-testid="quota-bars"]').text()).toContain('100%'); expect(w.get('[data-testid="quota-bars"]').text()).toContain('accountInsights.quota.disabled'); expect(w.get('[data-testid="quota-bars"]').text()).toContain('accountInsights.quota.unlimited'); expect(w.get('[data-testid="quota-bars"]').text()).toContain('dashboard.platformQuota.resetsAt') })
  it('keeps platform cards ordered and supports other card/simple mode', () => { const dashboard = stats({ total_actual_cost: 1, by_platform: [{ platform: 'kimi', total_requests: 1, total_tokens: 10, total_actual_cost: .2, today_requests: 1, today_tokens: 10, today_actual_cost: .2 }, { platform: 'openai', total_requests: 1, total_tokens: 10, total_actual_cost: .2, today_requests: 1, today_tokens: 10, today_actual_cost: .2 }] }); const w = render({ dashboardStats: dashboard }); expect(w.findAll('[data-testid="platform-card"]').map(x => x.attributes('data-platform'))).toEqual(['openai', 'kimi', '__other__']); const simple = render({ dashboardStats: dashboard, isSimple: true }); expect(simple.find('[data-testid="platform-card"]').exists()).toBe(false) })
  it('pluralizes the platform count for singular and plural values', () => {
    const one = render({ dashboardStats: stats({ by_platform: [{ platform: 'openai', total_requests: 1, total_tokens: 10, total_actual_cost: 1, today_requests: 1, today_tokens: 10, today_actual_cost: 1 }] }) })
    expect(one.text()).toContain('dashboard.platformCount:1')
    const two = render({ dashboardStats: stats({ by_platform: [{ platform: 'openai', total_requests: 1, total_tokens: 10, total_actual_cost: 1, today_requests: 1, today_tokens: 10, today_actual_cost: 1 }, { platform: 'anthropic', total_requests: 1, total_tokens: 10, total_actual_cost: 1, today_requests: 1, today_tokens: 10, today_actual_cost: 1 }] }) })
    expect(two.text()).toContain('dashboard.platformCount:2')
  })
  it('does not render an Other card when the platform response is empty', () => { const w = render({ dashboardStats: stats({ total_actual_cost: 10, by_platform: [] }) }); expect(w.text()).toContain('dashboard.platformCount'); expect(w.findAll('[data-testid="platform-card"]')).toHaveLength(0) })
  it('renders USD ledger and does not compare threshold when rate is unavailable', () => { currencyRate.value = null; const w = render({ lowBalance: { visible: false, thresholdCny: 210, dismissed: false } }); expect(w.get('[data-testid="account-balance"]').text()).toContain('$20.00'); expect(w.text()).toContain('accountInsights.warning.currencyHint'); expect(w.text()).not.toContain('accountInsights.warning.title') })
  it('never renders account cost fields', () => { expect(render().text().toLowerCase()).not.toContain('account_cost'); expect(render().text().toLowerCase()).not.toContain('账号成本') })
  it('uses four primary cards, a separate current-rate row, and four token boxes', () => {
    const w = render()
    expect(w.get('[data-testid="primary-metrics"]').findAll('[data-metric]').map(el => el.attributes('data-metric'))).toEqual(['rangeSpend', 'requests', 'tokens', 'averageResponse'])
    expect(w.get('[data-testid="current-rates"]').findAll('[data-metric]')).toHaveLength(2)
    expect(w.get('[data-testid="current-rates"]').text()).toContain('accountInsights.stats.recentAverage')
    expect(w.get('[data-testid="token-composition"]').findAll('[data-metric]').map(el => el.attributes('data-metric'))).toEqual(['freshInput', 'cacheCreation', 'cacheRead', 'output'])
    expect(w.get('[data-metric="rangeSpend"]').text()).toContain('accountInsights.stats.selectedRange')
  })
  it('formats the large-number fixture and preserves all eleven original values in native details', async () => {
    const w = render({
      rangeStats: range({ total_requests: 38081, total_actual_cost: 2122.42 / 7, total_cost: 9999, total_tokens: 6893947355, total_input_tokens: 487191835, total_cache_creation_tokens: 129913273, total_cache_read_tokens: 6254396153, total_output_tokens: 22446094, average_duration_ms: 13206.111 }),
      dashboardStats: stats({ today_actual_cost: 719.97 / 7, rpm: 2, tpm: 216293 }),
    })
    expect(w.get('[data-metric="rangeSpend"]').text()).toContain('¥2,122.42')
    expect(w.get('[data-testid="balance-card"]').text()).toContain('¥719.97')
    expect(w.get('[data-metric="requests"] .account-metric-value').text()).toBe('38,081')
    expect(w.get('[data-metric="tokens"] .account-metric-value').text()).toBe('6.89B')
    expect(w.get('[data-metric="averageResponse"] .account-metric-value').text()).toBe('13.21s')
    expect(w.get('[data-metric="tpm"] .account-metric-value').text()).toBe('216,293')
    expect(w.get('[data-metric="tokens"] .account-metric-value').attributes('aria-label')).toContain('6,893,947,355')
    expect(w.get('[data-metric="averageResponse"] .account-metric-value').attributes('title')).toContain('13,206.111 ms')
    const details = w.get('[data-testid="exact-values"]')
    expect(details.element.tagName).toBe('DETAILS')
    expect(details.find('summary').exists()).toBe(true)
    expect(details.attributes('open')).toBeUndefined()
    // Native summary supplies Enter/Space toggling in browsers without custom handlers.
    ;(details.element as HTMLDetailsElement).open = true
    await details.trigger('toggle')
    expect(details.findAll('[data-exact-metric]')).toHaveLength(11)
    expect(details.text()).toContain('6,893,947,355')
    expect(details.text()).toContain('13,206.111 ms')
    expect(details.text()).toContain('6,254,396,153')
    expect(details.text()).toContain('487,191,835')
    expect(details.text()).toContain('129,913,273')
    expect(details.text()).toContain('22,446,094')
    expect(details.get('[data-exact-metric="rangeSpend"]').text()).toContain('USD')
    expect(w.get('[data-metric="rangeSpend"]').text()).not.toContain('9999')
  })
  it('never replaces missing range fields with lifetime dashboard totals', () => {
    const w = render({ rangeStats: null, dashboardStats: stats({ total_requests: 99999, total_tokens: 88888, average_duration_ms: 77777 }) })
    for (const metric of ['rangeSpend', 'requests', 'tokens', 'averageResponse', 'freshInput', 'cacheCreation', 'cacheRead', 'output']) {
      expect(w.get(`[data-metric="${metric}"] .account-metric-value`).text()).toBe('—')
    }
    expect(w.get('[data-metric="rpm"] .account-metric-value').text()).toBe('4')
    expect(w.get('[data-testid="balance-card"]').text()).toContain('¥10.50')
  })
  it('keeps real zeros and guards nonfinite range values', () => {
    const w = render({ rangeStats: range({ total_actual_cost: 0, total_requests: 0, total_tokens: NaN, average_duration_ms: Infinity, total_input_tokens: 0 }) })
    expect(w.get('[data-metric="rangeSpend"] .account-metric-value').text()).toBe('¥0.00')
    expect(w.get('[data-metric="requests"] .account-metric-value').text()).toBe('0')
    expect(w.get('[data-metric="freshInput"] .account-metric-value').text()).toBe('0')
    expect(w.get('[data-metric="tokens"] .account-metric-value').text()).toBe('—')
    expect(w.get('[data-metric="averageResponse"] .account-metric-value').text()).toBe('—')
  })
  it('reserves two control rectangles beside key counts, including before profile arrives', () => {
    const w = mount(UserDashboardStats, { props: { profile: null }, slots: { 'date-control': '<select id="test-range" />', 'refresh-control': '<button id="test-refresh">Refresh</button>' }, global: { stubs: { UserDashboardBalanceWarning: true, UserDashboardRolling30d: true } } })
    expect(w.get('[data-testid="date-range-card"] #test-range').exists()).toBe(true)
    expect(w.get('[data-testid="refresh-settings-card"] #test-refresh').exists()).toBe(true)
    expect(w.get('[data-testid="account-overview"]').findAll('section')).toHaveLength(5)
    expect(w.find('meter').exists()).toBe(false)
    expect(w.text()).not.toContain('accountInsights.balance.frozen')
  })
  it('uses a clear two-decimal USD fallback for range spend', () => {
    currencyRate.value = null
    expect(render().get('[data-metric="rangeSpend"] .account-metric-value').text()).toBe('$9.00')
  })
})
