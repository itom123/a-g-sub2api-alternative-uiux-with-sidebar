import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { useAuthStore } from '@/stores/auth'
import DashboardView from '../DashboardView.vue'

const api = vi.hoisted(() => ({
  profile: vi.fn(), stats: vi.fn(), rangeStats: vi.fn(), trend: vi.fn(), snapshot: vi.fn(), query: vi.fn(), errors: vi.fn(), errorDetail: vi.fn(), quotas: vi.fn(), keys: vi.fn(), batch: vi.fn(), daily: vi.fn(), models: vi.fn(), auth: { user: { id: 1 } as { id: number } | null },
}))
vi.mock('@/stores/auth', async () => { const { reactive } = await import('vue'); return { useAuthStore: () => reactive(api.auth) } })
vi.mock('@/api/user', () => ({ getProfile: api.profile, getMyPlatformQuotas: api.quotas }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: api.keys } }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: api.stats, getStatsByRange: api.rangeStats, getDashboardTrend: api.trend, getDashboardSnapshotV2: api.snapshot, query: api.query, listMyErrorRequests: api.errors, getMyErrorDetail: api.errorDetail, getDashboardApiKeysUsage: api.batch, getMyApiKeyDailyUsage: api.daily, getDashboardModels: api.models } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, params?: unknown) => key + (params ? JSON.stringify(params) : '') }) }))

const options = { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' }, PageLoading: { template: '<div data-testid="page-loading" />' },
  UserDashboardStats: { props: ['rolling30'], template: '<div data-testid="stats-stub">{{ JSON.stringify(rolling30) }}</div>' }, UserDashboardCharts: { template: '<div data-testid="charts-stub" />' },
  UserDashboardKeyRanking: { template: '<div data-testid="keys-stub" />' }, UserDashboardKeyDrilldown: { template: '<div />' }, UserDashboardActiveNow: { template: '<div />' }, UserDashboardConsistency: { props: ['summary'], template: '<div data-testid="consistency-stub">{{ summary ? JSON.stringify(summary) : "" }}</div>' }, UserDashboardErrors: { emits: ['select-error'], template: '<button data-testid="open-error" @click="$emit(\'select-error\', 42)">error</button>' },
} } }
const wrappers: ReturnType<typeof mount>[] = []
function render() { const wrapper = mount(DashboardView, options); wrappers.push(wrapper); return wrapper }
function profile(id = 1, balance = 20) { return { id, balance } as any }
function stats() { return { total_api_keys: 2, active_api_keys: 1, total_requests: 3, total_input_tokens: 2, total_output_tokens: 3, total_cache_creation_tokens: 1, total_cache_read_tokens: 1, total_tokens: 7, total_actual_cost: 2, today_requests: 1, today_input_tokens: 1, today_output_tokens: 1, today_cache_creation_tokens: 0, today_cache_read_tokens: 0, today_tokens: 2, today_actual_cost: .5, average_duration_ms: 100, rpm: 1, tpm: 2, by_platform: [] } }
function rangeStats() { return { total_requests: 3, total_input_tokens: 2, total_output_tokens: 3, total_cache_tokens: 1, total_cache_read_tokens: 1, total_cache_creation_tokens: 0, total_tokens: 6, total_cost: 2, total_actual_cost: 2, average_duration_ms: 100 } }
function setup() {
  api.profile.mockResolvedValue(profile()); api.stats.mockResolvedValue(stats()); api.rangeStats.mockResolvedValue(rangeStats()); api.trend.mockResolvedValue({ trend: [] }); api.snapshot.mockResolvedValue({ trend: [], models: [], groups: [] }); api.query.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 1000, pages: 1 }); api.errors.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 10, pages: 1 }); api.quotas.mockResolvedValue({ platform_quotas: [] }); api.keys.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 1000, pages: 1 }); api.batch.mockResolvedValue({ stats: {} }); api.daily.mockResolvedValue({ items: [] }); api.models.mockResolvedValue({ models: [] }); api.errorDetail.mockResolvedValue({});
}
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-01T12:00:00Z')); vi.clearAllMocks(); localStorage.clear(); api.auth.user = { id: 1 }; Object.defineProperty(document, 'hidden', { configurable: true, value: false }); setup() })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers() })

const calls = () => JSON.stringify({ get: api.profile.mock.calls, stats: api.stats.mock.calls, range: api.rangeStats.mock.calls, trend: api.trend.mock.calls, snapshot: api.snapshot.mock.calls, query: api.query.mock.calls, errors: api.errors.mock.calls, quotas: api.quotas.mock.calls, keys: api.keys.mock.calls, batch: api.batch.mock.calls })

describe('DashboardView orchestration', () => {
  it('places recent errors in a full-width desktop row while preserving mobile stacking', async () => {
    const wrapper = render(); await flushPromises()
    const errorsRow = wrapper.get('[data-testid="account-errors-row"]')
    expect(errorsRow.classes()).toContain('lg:col-span-2')
    expect(errorsRow.element.parentElement?.className).toContain('lg:grid-cols-2')
    expect(errorsRow.element.parentElement?.className).not.toContain('xl:grid-cols-2')
  })
  it('supports all six presets and rejects reversed or 31-day custom ranges', async () => { const wrapper = render(); await flushPromises(); const select = wrapper.get('#account-range-preset'); const before = api.rangeStats.mock.calls.length; for (const preset of ['today', '7d', 'thisWeek', 'lastWeek', 'thisMonth', '30d']) { await select.setValue(preset); await flushPromises() } expect(api.rangeStats.mock.calls.length).toBeGreaterThanOrEqual(before + 12); const afterPresets = api.rangeStats.mock.calls.length; await select.setValue('custom'); await flushPromises(); const dates = wrapper.findAll('input[type=date]'); await dates[0].setValue('2026-11-02'); await dates[1].setValue('2026-11-01'); await flushPromises(); expect(wrapper.get('[role=alert]').text()).toContain('accountInsights.range.invalid'); expect(api.rangeStats.mock.calls.length).toBe(afterPresets + 2); await dates[0].setValue('2026-10-01'); await dates[1].setValue('2026-11-01'); await flushPromises(); expect(wrapper.get('[role=alert]').text()).toContain('accountInsights.range.invalid'); expect(api.rangeStats.mock.calls.length).toBe(afterPresets + 2) })
  it('defaults to 30 seconds and maps the legacy opt-out to off', async () => { const wrapper = render(); await flushPromises(); expect((wrapper.get('#account-refresh-interval').element as HTMLSelectElement).value).toBe('30'); await wrapper.get('#account-refresh-interval').setValue('900'); expect(localStorage.getItem('account-insights:refresh-interval:1')).toBe('900'); wrapper.unmount(); localStorage.clear(); localStorage.setItem('account-usage-auto-refresh:1', 'false'); const next = render(); await flushPromises(); expect((next.get('#account-refresh-interval').element as HTMLSelectElement).value).toBe('0') })
  it('supports every refresh interval choice', async () => { const wrapper = render(); await flushPromises(); for (const value of ['0', '30', '60', '300', '900']) { await wrapper.get('#account-refresh-interval').setValue(value); expect((wrapper.get('#account-refresh-interval').element as HTMLSelectElement).value).toBe(value) } })
  it('pauses while hidden and refreshes when visible', async () => { render(); await flushPromises(); const initial = api.stats.mock.calls.length; Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')); await vi.advanceTimersByTimeAsync(60000); expect(api.stats).toHaveBeenCalledTimes(initial); Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')); await flushPromises(); expect(api.stats).toHaveBeenCalledTimes(initial + 1) })
  it('retains the last good snapshot and shows a stale notice after optional failure', async () => { const wrapper = render(); await flushPromises(); api.snapshot.mockRejectedValueOnce(new Error('offline')); await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises(); expect(wrapper.get('[role=status]').text()).toContain('accountInsights.staleNotice'); expect(wrapper.find('[data-testid="stats-stub"]').exists()).toBe(true) })
  it('shows a retryable error when a required initial request fails', async () => { api.profile.mockRejectedValueOnce(new Error('offline')); const wrapper = render(); await flushPromises(); expect(wrapper.get('[role=alert]').text()).toContain('accountInsights.loadFailed'); expect(wrapper.find('[data-testid="stats-stub"]').exists()).toBe(false) })
  it('does not overlap automatic calls and cleans up on unmount', async () => { const wrapper = render(); await flushPromises(); const pending = new Promise<ReturnType<typeof stats>>(() => {}) ; api.stats.mockReturnValueOnce(pending); await wrapper.get('button.btn-secondary').trigger('click'); await vi.advanceTimersByTimeAsync(90000); expect(api.stats).toHaveBeenCalledTimes(2); const count = api.stats.mock.calls.length; wrapper.unmount(); await vi.advanceTimersByTimeAsync(90000); expect(api.stats.mock.calls.length).toBe(count) })
  it('ignores stale responses from an older refresh', async () => { const wrapper = render(); await flushPromises(); let resolveOld!: (value: any) => void; const old = new Promise(resolve => { resolveOld = resolve }); api.snapshot.mockReturnValueOnce(old).mockResolvedValueOnce({ trend: [{ date: 'new' }], models: [], groups: [] }); await wrapper.get('button.btn-secondary').trigger('click'); await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises(); resolveOld({ trend: [{ date: 'old' }], models: [], groups: [] }); await flushPromises(); expect(wrapper.find('[role=status]').exists()).toBe(false) })
  it('clears the old account on account switch and commits only the new profile', async () => { const wrapper = render(); await flushPromises(); api.profile.mockReturnValueOnce(new Promise(() => {})); useAuthStore().user = { id: 2 }; await nextTick(); await flushPromises(); expect(api.profile.mock.calls.length).toBeGreaterThanOrEqual(2); expect(wrapper.find('[data-testid="stats-stub"]').exists()).toBe(false) })
  it('feeds rolling and consistency cards from identical 30-day totals and keeps them fixed across presets', async () => {
    api.rangeStats.mockImplementation(async (params: { start_date: string }) => params.start_date === '2026-10-03' ? { total_requests: 40, total_input_tokens: 0, total_output_tokens: 0, total_cache_tokens: 0, total_cache_read_tokens: 0, total_cache_creation_tokens: 0, total_tokens: 10500, total_cost: 3, total_actual_cost: 3, average_duration_ms: 100 } : rangeStats())
    api.trend.mockResolvedValue({ trend: Array.from({ length: 7 }, (_, index) => ({ date: `2026-10-${25 + index}`, requests: index < 5 ? 6 : 5, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: index < 5 ? 1575 : 1312.5, cost: 0, actual_cost: 0 })) })
    const wrapper = render(); await flushPromises()
    const first = wrapper.get('[data-testid="consistency-stub"]').text()
    expect(first).toContain('"requests":40')
    expect(first).toContain('"activeDays30":7')
    expect(first).toContain('"callsPerActiveDay":5.714')
    expect(first).toContain('"tokensPerCall":263')
    const rolling = wrapper.find('[data-testid="stats-stub"]')
    expect(rolling.text()).toContain('"requests":40')
    expect(rolling.text()).toContain('"tokensPerCall":263')
    const rangePreset = wrapper.get('#account-range-preset'); await rangePreset.setValue('today'); await flushPromises()
    expect(wrapper.get('[data-testid="consistency-stub"]').text()).toBe(first)
    expect(wrapper.find('[data-testid="stats-stub"]').text()).toContain('"requests":40')
    expect(wrapper.find('[data-testid="stats-stub"]').text()).toContain('"tokensPerCall":263')
  })
  it('renders fetched error details while keeping the response body hidden until requested', async () => {
    api.errors.mockResolvedValueOnce({ items: [{ id: 42, created_at: '2026-11-01T11:00:00Z', model: 'model-a', inbound_endpoint: '/v1/messages', status_code: 502, category: 'upstream', platform: 'anthropic', message: 'safe message', key_name: 'key-a' }], total: 1, page: 1, page_size: 10, pages: 1 })
    api.errorDetail.mockResolvedValueOnce({ id: 42, created_at: '2026-11-01T11:00:00Z', model: 'model-a', inbound_endpoint: '/v1/messages', status_code: 502, category: 'upstream', platform: 'anthropic', message: 'safe message', key_name: 'key-a', key_deleted: false, error_body: '{"secret":"hidden"}', upstream_status_code: 503 })
    const wrapper = render(); await flushPromises(); await wrapper.get('[data-testid="open-error"]').trigger('click'); await flushPromises()
    expect(api.errorDetail).toHaveBeenCalledWith(42); expect(wrapper.get('[data-testid="account-error-detail"]').text()).toContain('safe message'); expect(wrapper.get('[data-testid="account-error-detail"]').text()).toContain('503'); expect(wrapper.find('[data-testid="account-error-body"]').exists()).toBe(false)
    await wrapper.get('[data-testid="account-error-body-toggle"]').trigger('click'); expect(wrapper.get('[data-testid="account-error-body"]').text()).toContain('hidden')
  })
  it('queues exactly one manual refresh while a refresh is in flight', async () => {
    const wrapper = render(); await flushPromises(); let resolveStats!: (value: any) => void; api.stats.mockReturnValueOnce(new Promise(resolve => { resolveStats = resolve }))
    await wrapper.get('button.btn-secondary').trigger('click'); await wrapper.get('button.btn-secondary').trigger('click'); await wrapper.get('button.btn-secondary').trigger('click'); expect(api.stats).toHaveBeenCalledTimes(2)
    resolveStats(stats()); await flushPromises(); expect(api.stats).toHaveBeenCalledTimes(3); await flushPromises(); expect(api.stats).toHaveBeenCalledTimes(3)
  })
  it('never sends a user_id field in any captured request', async () => { render(); await flushPromises(); expect(calls()).not.toMatch(/user_id/) })
})
