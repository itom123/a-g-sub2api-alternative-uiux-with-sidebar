import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GatewayDevices from './GatewayDevices.vue'
import { getOpenClawGatewayDevices, updateOpenClawGatewayDevice } from '@/api/openclaw'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/openclaw', () => ({ getOpenClawGatewayDevices: vi.fn(), updateOpenClawGatewayDevice: vi.fn() }))

const pending = { deviceId: 'device-12345678', requestId: 'request-current', displayName: 'New browser', role: 'operator', scopes: ['operator.read'] }
describe('GatewayDevices', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getOpenClawGatewayDevices).mockResolvedValue({ pending: [pending], paired: [] })
    vi.mocked(updateOpenClawGatewayDevice).mockResolvedValue()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })
  it('shows requested permissions and submits the exact reviewed device identity', async () => {
    const wrapper = mount(GatewayDevices, { props: { instanceId: 7 } })
    await flushPromises()
    expect(wrapper.text()).toContain('operator.read')
    expect(wrapper.text()).toContain('request-current')
    expect(wrapper.text()).toContain('device-12345678')
    const approve = wrapper.findAll('button').find(b => b.text() === 'connections.gatewayDevices.approve')!
    await approve.trigger('click')
    await flushPromises()
    expect(updateOpenClawGatewayDevice).toHaveBeenCalledWith(7, 'approve', pending)
    wrapper.unmount()
  })
  it('clears stale requests and displays a safe next action after a failed mutation', async () => {
    vi.mocked(updateOpenClawGatewayDevice).mockRejectedValue(new Error('secret internal upstream'))
    const wrapper = mount(GatewayDevices, { props: { instanceId: 7 } })
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'connections.gatewayDevices.reject')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('secret internal upstream')
    expect(wrapper.text()).not.toContain('request-current')
    expect(wrapper.get('[role="alert"]').text()).toContain('connections.gatewayDevices.failed')
    wrapper.unmount()
  })
  it('requires confirmation before revoking a device', async () => {
    const paired = { deviceId: 'paired-12345678', displayName: 'Owner browser', roles: ['operator'] }
    vi.mocked(getOpenClawGatewayDevices).mockResolvedValue({ pending: [], paired: [paired] })
    vi.mocked(window.confirm).mockReturnValue(false)
    const wrapper = mount(GatewayDevices, { props: { instanceId: 7 } })
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'connections.gatewayDevices.revoke')!.trigger('click')
    expect(updateOpenClawGatewayDevice).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
