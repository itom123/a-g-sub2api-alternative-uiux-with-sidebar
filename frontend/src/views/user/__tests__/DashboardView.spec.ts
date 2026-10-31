import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth'
import DashboardView from '../DashboardView.vue'

const api = vi.hoisted(() => ({
  profile: vi.fn(), stats: vi.fn(), trend: vi.fn(), models: vi.fn(), logs: vi.fn(), quotas: vi.fn(),
  auth: { user: { id: 1 }, isSimpleMode: false },
}))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  return { useAuthStore: () => reactive(api.auth) }
})
vi.mock('@/api/user', () => ({ getProfile: api.profile, getMyPlatformQuotas: api.quotas }))
vi.mock('@/api/usage', () => ({ usageAPI: {
  getDashboardStats: api.stats, getDashboardTrend: api.trend, getDashboardModels: api.models, query: api.logs,
} }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: unknown) => key + (params ? JSON.stringify(params) : '') }),
}))

const options = { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' }, PageLoading: true,
  UserDashboardStats: true, UserDashboardCharts: true, UserDashboardRecentUsage: true,
} } }
const wrappers: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(DashboardView, options)
  wrappers.push(wrapper)
  return wrapper
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-31T12:00:00Z'))
  vi.clearAllMocks()
  localStorage.clear()
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  api.auth.user = { id: 1 }
  api.profile.mockResolvedValue({ id: 1, balance: 20 })
  api.stats.mockResolvedValue({ today_requests: 3 })
  api.trend.mockResolvedValue({ trend: [{ date: '2026-10-31', actual_cost: 2 }] })
  api.models.mockResolvedValue({ models: [{ model: 'example', actual_cost: 2 }] })
  api.logs.mockResolvedValue({ items: [{ id: 10 }] })
  api.quotas.mockResolvedValue({ platform_quotas: [] })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.useRealTimers()
})

describe('current account usage and balance', () => {
  it('loads one account snapshot with bounded, newest-first calls and no user selector', async () => {
    const wrapper = render()
    await flushPromises()
    expect(api.logs).toHaveBeenCalledWith({ start_date: '2026-10-25', end_date: '2026-10-31', page: 1, page_size: 10, sort_by: 'created_at', sort_order: 'desc' })
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).props('balance')).toBe(20)
    expect(wrapper.findComponent({ name: 'UserDashboardRecentUsage' }).props('startDate')).toBe('2026-10-25')
    expect(wrapper.get('input[type=checkbox]').element).toHaveProperty('checked', true)
  })

  it('refreshes balance, charts, quota and calls after 30 seconds', async () => {
    const wrapper = render()
    await flushPromises()
    api.profile.mockResolvedValue({ id: 1, balance: 19 })
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    for (const fn of [api.profile, api.stats, api.trend, api.models, api.logs, api.quotas]) expect(fn).toHaveBeenCalledTimes(2)
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).props('balance')).toBe(19)
  })

  it('pauses when hidden and refreshes immediately on return', async () => {
    render()
    await flushPromises()
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    await vi.advanceTimersByTimeAsync(60000)
    expect(api.stats).toHaveBeenCalledTimes(1)
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(api.stats).toHaveBeenCalledTimes(2)
  })

  it('retains a failed snapshot and labels it stale instead of showing zeros', async () => {
    const wrapper = render()
    await flushPromises()
    api.profile.mockResolvedValue({ id: 1, balance: 0 })
    api.quotas.mockRejectedValue(new Error('offline'))
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('dashboard.refreshFailed')
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).props('balance')).toBe(20)
    api.quotas.mockResolvedValue({ platform_quotas: [] })
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).props('balance')).toBe(0)
  })

  it('shows a retryable initial error without inventing an empty account', async () => {
    api.profile.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('dashboard.loadFailed')
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).exists()).toBe(false)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  })

  it('does not overlap automatic refreshes or keep polling after leaving', async () => {
    const pending = deferred<{ trend: never[] }>()
    api.trend.mockReturnValue(pending.promise)
    const wrapper = render()
    await vi.advanceTimersByTimeAsync(90000)
    expect(api.trend).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    pending.resolve({ trend: [] })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(90000)
    expect(api.trend).toHaveBeenCalledTimes(1)
  })

  it('ignores an older date-range response that arrives last', async () => {
    const wrapper = render()
    await flushPromises()
    const old = deferred<{ trend: { date: string }[] }>()
    api.trend.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ trend: [{ date: 'new' }] })
    const charts = wrapper.findComponent({ name: 'UserDashboardCharts' })
    charts.vm.$emit('update:startDate', '2026-10-01')
    charts.vm.$emit('dateRangeChange')
    charts.vm.$emit('update:startDate', '2026-10-20')
    charts.vm.$emit('dateRangeChange')
    await flushPromises()
    old.resolve({ trend: [{ date: 'old' }] })
    await flushPromises()
    expect(charts.props('trend')).toEqual([{ date: 'new' }])
    expect(wrapper.findComponent({ name: 'UserDashboardRecentUsage' }).props('startDate')).toBe('2026-10-20')
  })

  it('clears the old account before loading a different signed-in user', async () => {
    const wrapper = render()
    await flushPromises()
    const pending = deferred<{ id: number; balance: number }>()
    api.profile.mockReturnValueOnce(pending.promise)
    useAuthStore().user = { id: 2 } as ReturnType<typeof useAuthStore>['user']
    await flushPromises()
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).exists()).toBe(false)
    pending.resolve({ id: 2, balance: 3 })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'UserDashboardStats' }).props('balance')).toBe(3)
  })

  it('remembers an account opting out of automatic refresh', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('input[type=checkbox]').setValue(false)
    wrapper.unmount()
    const next = render()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60000)
    expect(api.stats).toHaveBeenCalledTimes(2)
    expect(next.get('input[type=checkbox]').element).toHaveProperty('checked', false)
  })
})
