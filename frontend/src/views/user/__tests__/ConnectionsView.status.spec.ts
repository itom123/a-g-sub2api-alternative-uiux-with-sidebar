import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OpenClawDashboard, OpenClawInstance } from '@/api/openclaw'
import { currencyRate } from '@/utils/currency'
import ConnectionsView from '../ConnectionsView.vue'

const api = vi.hoisted(() => ({ lifecycle: vi.fn(), dashboard: vi.fn(), kinds: vi.fn(), connections: vi.fn(), provision: vi.fn() }))
vi.mock('@/api/openclaw', () => ({
  getOpenClawLifecycle: api.lifecycle, getOpenClawDashboard: api.dashboard,
  listOpenClawConnectionKinds: api.kinds, listOpenClawConnections: api.connections,
  provisionOpenClawInstance: api.provision, deleteOpenClawConnection: vi.fn(), deleteOpenClawInstance: vi.fn(),
  createOpenClawConnection: vi.fn(), completeOpenClawConnection: vi.fn(), createOpenClawAccessRequest: vi.fn(),
  createOpenClawInstance: vi.fn(), getOpenClawGatewayLaunch: vi.fn(), renameOpenClawInstance: vi.fn(),
  startOpenClawInstance: vi.fn(), stopOpenClawInstance: vi.fn(),
}))
vi.mock('@/composables/useTelegramAvailability', () => ({ useTelegramAvailability: () => ({ telegramAvailable: false }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const wrappers: ReturnType<typeof mount>[] = []
function instance(status: string): OpenClawInstance {
  return { id: 7, tenant_slug: 'u42-0123456789ab', name: 'Workspace', status, worker_host: 'default', created_at: '2026-11-01T01:00:00Z', updated_at: '2026-11-01T02:00:00Z' }
}
function setup(oldState: string, currentState: string, fleetState = currentState) {
  const current = instance(currentState)
  api.lifecycle.mockResolvedValue({ instance: instance(oldState), can_create: false, can_delete: true, self_service_enabled: true })
  const dashboard: OpenClawDashboard = { instance: current, fleet_state: fleetState, checked_at: '2026-11-01T03:00:00Z', gateway_enabled: true, connection_count: 0, connected_count: 0 }
  api.dashboard.mockResolvedValue(dashboard)
  return dashboard
}
async function render() {
  const wrapper = mount(ConnectionsView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' }, BaseDialog: true, GatewayDevices: true, GatewayEnrollment: true, RouterLink: true,
  } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function actions(wrapper: ReturnType<typeof mount>) { return wrapper.get('.tenant-actions').text() }
beforeEach(() => { vi.resetAllMocks(); api.kinds.mockResolvedValue([]); api.connections.mockResolvedValue([]) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('Connections workspace billing', () => {
  afterEach(() => { currencyRate.value = null })
  it('shows the charged amount in CNY like the balance, not raw USD', async () => {
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    const dashboard = setup('running', 'running')
    api.dashboard.mockResolvedValue({ ...dashboard, api_key: { id: 3, name: 'Billing key', status: 'active', quota: 0, quota_used: 0, usage_count: 1284, usage_cost: 51.9634 } })
    const wrapper = await render()
    const stats = wrapper.get('.billing-api-stats').text()
    expect(stats).toContain('¥363.74')
    expect(stats).toContain('1,284')
    expect(stats).not.toContain('$')
  })
  it('shows a dash rather than a guessed amount when the exchange rate is unavailable', async () => {
    currencyRate.value = null
    const dashboard = setup('running', 'running')
    api.dashboard.mockResolvedValue({ ...dashboard, api_key: { id: 3, name: 'Billing key', status: 'active', quota: 0, quota_used: 0, usage_count: 1, usage_cost: 1 } })
    const wrapper = await render()
    expect(wrapper.get('.billing-api-stats').text()).toContain('connections.billingApiSpent—')
  })
})

describe('Connections workspace activity', () => {
  const key = { id: 3, name: 'Billing key', status: 'active', quota: 0, quota_used: 0, usage_count: 4, usage_cost: 1 }
  function activityCard(wrapper: ReturnType<typeof mount>) {
    const card = wrapper.findAll('.metric-card').find(item => item.text().includes('connections.metrics.activity'))
    if (!card) throw new Error('activity card missing')
    return card
  }
  it('shows the workspace key\'s latest ledger request as the latest activity', async () => {
    const dashboard = setup('running', 'running')
    api.dashboard.mockResolvedValue({ ...dashboard, api_key: { ...key, last_used_at: '2026-11-01T02:59:00Z', last_request_at: '2026-11-01T02:58:00Z' } })
    const card = activityCard(await render())
    expect(card.get('time').attributes('datetime')).toBe('2026-11-01T02:58:00Z')
    expect(card.text()).not.toContain('connections.metrics.noActivity')
  })
  it('says there are no requests when the ledger has none, even if the key was touched', async () => {
    const dashboard = setup('running', 'running')
    api.dashboard.mockResolvedValue({ ...dashboard, api_key: { ...key, usage_count: 0, last_used_at: '2026-11-01T02:59:00Z' } })
    const card = activityCard(await render())
    expect(card.find('time').exists()).toBe(false)
    expect(card.text()).toContain('connections.metrics.noActivity')
  })
})

describe('Connections workspace status', () => {
  it('uses the refreshed running instance for controls immediately', async () => {
    setup('missing', 'running')
    const wrapper = await render()
    expect(wrapper.get('.state-pill').text()).toBe('connections.instanceStatus.running')
    expect(actions(wrapper)).toContain('connections.openGateway')
    expect(actions(wrapper)).toContain('connections.stop')
    expect(actions(wrapper)).not.toContain('connections.start')
    expect(actions(wrapper)).not.toContain('connections.provisionAction')
    expect(wrapper.find('.provision-panel').exists()).toBe(false)
  })
  it('ignores a delayed older refresh after a newer stopped observation', async () => {
    const running = setup('running', 'running')
    const wrapper = await render()
    let resolveOld!: (value: OpenClawDashboard) => void
    api.dashboard.mockReturnValueOnce(new Promise<OpenClawDashboard>(resolve => { resolveOld = resolve }))
    const reload = (wrapper.vm as unknown as { load: () => Promise<void> }).load
    const oldLoad = reload()
    await flushPromises()
    const stopped = { ...running, instance: { ...running.instance, status: 'stopped' }, fleet_state: 'stopped' }
    api.dashboard.mockResolvedValueOnce(stopped)
    await reload()
    await flushPromises()
    expect(actions(wrapper)).toContain('connections.start')
    resolveOld(running)
    await oldLoad
    await flushPromises()
    expect(wrapper.get('.state-pill').text()).toBe('connections.instanceStatus.stopped')
    expect(actions(wrapper)).toContain('connections.start')
    expect(actions(wrapper)).not.toContain('connections.stop')
    expect(actions(wrapper)).not.toContain('connections.openGateway')
  })
  it('uses the refreshed stopped instance rather than stale running controls', async () => {
    setup('running', 'stopped')
    const wrapper = await render()
    expect(wrapper.get('.state-pill').text()).toBe('connections.instanceStatus.stopped')
    expect(actions(wrapper)).toContain('connections.start')
    expect(actions(wrapper)).not.toContain('connections.stop')
    expect(actions(wrapper)).not.toContain('connections.openGateway')
  })
  it('never offers provisioning for an unavailable observation', async () => {
    setup('missing', 'missing', 'unavailable')
    const wrapper = await render()
    expect(wrapper.get('.state-pill').text()).toBe('connections.instanceStatus.unavailable')
    expect(wrapper.find('.provision-panel').exists()).toBe(false)
    expect(actions(wrapper)).not.toContain('connections.provisionAction')
    expect(api.provision).not.toHaveBeenCalled()
  })
  it('retains last-known running controls during a bridge outage without setup prompts', async () => {
    setup('running', 'running', 'unavailable')
    const wrapper = await render()
    expect(actions(wrapper)).toContain('connections.stop')
    expect(wrapper.find('.provision-panel').exists()).toBe(false)
    expect(actions(wrapper)).not.toContain('connections.provisionAction')
  })
  it.each(['planned', 'missing', 'error'])('preserves actual %s setup retries', async state => {
    setup(state, state)
    const wrapper = await render()
    expect(wrapper.find('.provision-panel').exists()).toBe(true)
    expect(actions(wrapper)).toContain('connections.provisionAction')
  })
  it('preserves deletion scheduling returned during a status refresh', async () => {
    const dashboard = setup('running', 'deletion_scheduled', 'running')
    dashboard.instance.deletion_scheduled_at = '2026-11-08T02:00:00Z'
    const wrapper = await render()
    expect(wrapper.get('.state-pill').text()).toBe('connections.instanceStatus.deletion_scheduled')
    expect(actions(wrapper)).toContain('connections.deleteQueuedTitle')
    expect(actions(wrapper)).not.toContain('connections.stop')
    expect(actions(wrapper)).not.toContain('connections.openGateway')
    expect(wrapper.find('.provision-panel').exists()).toBe(false)
  })
  it('never shows missing-cell setup while deletion is queued', async () => {
    const dashboard = setup('deletion_scheduled', 'deletion_scheduled', 'missing')
    dashboard.instance.deletion_scheduled_at = '2026-11-08T02:00:00Z'
    const wrapper = await render()
    expect(wrapper.find('.provision-panel').exists()).toBe(false)
    expect(actions(wrapper)).not.toContain('connections.provisionAction')
  })
})
