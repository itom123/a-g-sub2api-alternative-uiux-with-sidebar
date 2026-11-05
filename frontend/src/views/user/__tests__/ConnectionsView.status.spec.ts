import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OpenClawDashboard, OpenClawInstance } from '@/api/openclaw'
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
  const dashboard: OpenClawDashboard = { instance: current, fleet_state: fleetState, checked_at: '2026-11-01T03:00:00Z', gateway_enabled: true, connection_count: 0, connected_count: 0, usage_count: 0 }
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
