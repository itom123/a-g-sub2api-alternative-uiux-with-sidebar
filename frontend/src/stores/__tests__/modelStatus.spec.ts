import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useModelStatusStore } from '../modelStatus'
const mock = vi.hoisted(() => ({ catalog: vi.fn(), channels: vi.fn(), rates: vi.fn(), snapshot: vi.fn(), models: vi.fn(), matrix: vi.fn(), dimensions: vi.fn(), auth: {} as any, app: {} as any }))
vi.mock('../auth', () => ({ useAuthStore: () => mock.auth }))
vi.mock('../app', () => ({ useAppStore: () => mock.app }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza: mock.catalog }))
vi.mock('@/api/channels', () => ({ getAvailable: mock.channels }))
vi.mock('@/api/groups', () => ({ getUserGroupRates: mock.rates }))
vi.mock('@/api/channelMonitorV2', () => ({ getSnapshot: mock.snapshot, getModels: mock.models, getMatrix: mock.matrix, getDimensions: mock.dimensions }))
const coverage = () => ({ data_through: new Date().toISOString(), computed_at: new Date().toISOString(), coverage_complete: false, aggregation_lag_seconds: 0, bootstrap: { active: true, progress_percent: 20 } })
const row = (health = 'unknown', count = 2) => ({ platform: 'openai', model: 'luna', metrics: { request_count: count, ttft: { p50_ms: null, sample_count: 0 } }, health: { overall: health, minimum_sample: 50 } })
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks(); setActivePinia(createPinia())
  mock.auth = reactive({ token: 'session-a', isAuthenticated: true })
  mock.app = reactive({ cachedPublicSettings: { model_plaza_enabled: true, model_plaza_require_auth: true, available_channels_enabled: true, channel_monitor_enabled: true, channel_monitor_mode: 'v2' } })
  mock.catalog.mockResolvedValue({ groups: [{ id: 8, models: [{ name: 'luna' }] }] })
  mock.channels.mockResolvedValue([]); mock.rates.mockResolvedValue({ 8: 0 })
  mock.snapshot.mockResolvedValue({ coverage: coverage(), config: { refresh_interval_seconds: 60 }, health: { overall: 'unknown' } })
  mock.models.mockResolvedValue({ coverage: coverage(), items: [row()] })
  mock.matrix.mockResolvedValue({ items: [], coverage: coverage() }); mock.dimensions.mockResolvedValue({ platforms: [], groups: [], models: [] })
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
})
afterEach(() => { vi.useRealTimers() })
describe('permission-scoped shared live status', () => {
  it('uses exact platform/model joins, preserving unknown and no sample latency', async () => {
    const live = useModelStatusStore(); await live.refresh()
    expect(live.findModel('openai', 'luna')?.health.overall).toBe('unknown')
    expect(live.findModel('openai', 'Luna')).toBeUndefined()
    expect(live.findModel('anthropic', 'luna')).toBeUndefined()
    expect(live.rates[8]).toBe(0)
    expect(live.snapshot?.coverage.coverage_complete).toBe(false)
    expect(mock.snapshot.mock.calls[0][1]).toBe(false)
  })
  it('refreshes model permissions and health while retaining catalog on status failure', async () => {
    const live = useModelStatusStore(); await live.refresh()
    mock.catalog.mockResolvedValue({ groups: [{ id: 9, models: [{ name: 'sol' }] }] })
    mock.models.mockResolvedValue({ items: [row('warning', 60)] })
    await live.refresh(); expect(live.catalog?.groups[0].id).toBe(9)
    expect(live.findModel('openai', 'luna')?.health.overall).toBe('warning')
    mock.snapshot.mockRejectedValue({ status: 503 }); await live.refresh()
    expect(live.stale).toBe(true); expect(live.statusError).toBe(true)
    expect(live.snapshot).not.toBeNull(); expect(live.catalog?.groups[0].id).toBe(9)
  })
  it('clears private data on session change and ignores obsolete responses', async () => {
    const live = useModelStatusStore(); await live.refresh()
    let resolve!: (value: any) => void
    mock.catalog.mockImplementation(() => new Promise(r => { resolve = r }))
    const pending = live.refresh(); mock.auth.token = 'session-b'
    expect(live.catalog).toBeNull(); expect(live.models).toEqual([])
    resolve({ groups: [{ id: 999 }] }); await pending; expect(live.catalog).toBeNull()
  })
  it.each([401, 403])('removes cached data on access denial %s', async status => {
    const live = useModelStatusStore(); await live.refresh()
    mock.models.mockRejectedValue({ status }); await live.refresh()
    expect(live.catalog).toBeNull(); expect(live.channels).toEqual([]); expect(live.snapshot).toBeNull()
  })
  it('uses one poller, pauses hidden tabs and refreshes on return', async () => {
    const live = useModelStatusStore(); live.retain(); live.retain()
    await vi.advanceTimersByTimeAsync(0); expect(mock.snapshot).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(60000); expect(mock.snapshot).toHaveBeenCalledTimes(2)
    Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(120000); expect(mock.snapshot).toHaveBeenCalledTimes(2)
    Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0); expect(mock.snapshot).toHaveBeenCalledTimes(3)
    live.release(); live.release(); await vi.advanceTimersByTimeAsync(60000); expect(mock.snapshot).toHaveBeenCalledTimes(3)
  })
  it('does not fetch private endpoints for anonymous visitors', async () => {
    mock.auth.isAuthenticated = false; mock.auth.token = ''
    const live = useModelStatusStore(); await live.refresh()
    expect(mock.catalog).not.toHaveBeenCalled(); expect(mock.channels).not.toHaveBeenCalled(); expect(mock.snapshot).not.toHaveBeenCalled()
  })
  it('distinguishes the zero-date empty scope from a stale real observation', async () => {
    mock.snapshot.mockResolvedValue({ coverage: { ...coverage(), data_through: '0001-01-01T00:00:00Z' }, config: { refresh_interval_seconds: 60 } })
    const live = useModelStatusStore(); await live.refresh()
    expect(live.hasObservation).toBe(false); expect(live.stale).toBe(false)
    mock.snapshot.mockResolvedValue({ coverage: { ...coverage(), data_through: new Date(Date.now() - 180000).toISOString() }, config: { refresh_interval_seconds: 60 } })
    await live.refresh(); expect(live.hasObservation).toBe(true); expect(live.stale).toBe(true)
  })
})
