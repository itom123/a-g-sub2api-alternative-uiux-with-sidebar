import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OpenClawConnection, OpenClawConnectionKindInfo, OpenClawInstance } from '@/api/openclaw'
import ConnectionsView from '../ConnectionsView.vue'

const api = vi.hoisted(() => ({
  lifecycle: vi.fn(), dashboard: vi.fn(), kinds: vi.fn(), connections: vi.fn(),
  create: vi.fn(), complete: vi.fn(), remove: vi.fn(), qr: vi.fn(),
}))
vi.mock('@/api/openclaw', () => ({
  getOpenClawLifecycle: api.lifecycle, getOpenClawDashboard: api.dashboard,
  listOpenClawConnectionKinds: api.kinds, listOpenClawConnections: api.connections,
  provisionOpenClawInstance: vi.fn(), deleteOpenClawConnection: api.remove, deleteOpenClawInstance: vi.fn(),
  createOpenClawConnection: api.create, completeOpenClawConnection: api.complete, createOpenClawAccessRequest: vi.fn(),
  createOpenClawInstance: vi.fn(), getOpenClawGatewayLaunch: vi.fn(), renameOpenClawInstance: vi.fn(),
  startOpenClawInstance: vi.fn(), stopOpenClawInstance: vi.fn(),
}))
vi.mock('qrcode', () => ({ default: { toDataURL: api.qr } }))
vi.mock('@/composables/useTelegramAvailability', () => ({ useTelegramAvailability: () => ({ telegramAvailable: false }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const URL_A = 'https://liteapp.weixin.qq.com/q/AAA?qrcode=a&bot_type=3'
const URL_B = 'https://liteapp.weixin.qq.com/q/BBB?qrcode=b&bot_type=3'
const wechatKind: OpenClawConnectionKindInfo = { kind: 'wechat', title: 'WeChat', setup_available: true } as OpenClawConnectionKindInfo
const telegramKind: OpenClawConnectionKindInfo = { kind: 'telegram', title: 'Telegram', setup_available: true } as OpenClawConnectionKindInfo

function conn(over: Partial<OpenClawConnection> = {}): OpenClawConnection {
  return { id: 5, instance_id: 7, kind: 'wechat', status: 'pending', label: '', usage_count: 0, created_at: '2026-11-01T01:00:00Z', updated_at: '2026-11-01T01:00:00Z', ...over }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const wrappers: ReturnType<typeof mount>[] = []
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms) }
async function render(existing: OpenClawConnection[] = []) {
  const instance: OpenClawInstance = { id: 7, tenant_slug: 'u42', name: 'Workspace', status: 'running', worker_host: 'default', created_at: '2026-11-01T01:00:00Z', updated_at: '2026-11-01T02:00:00Z' }
  api.lifecycle.mockResolvedValue({ instance, can_create: false, can_delete: true, self_service_enabled: true })
  api.dashboard.mockResolvedValue({ instance, fleet_state: 'running', checked_at: '2026-11-01T03:00:00Z', gateway_enabled: true, connection_count: 0, connected_count: 0, usage_count: 0 })
  api.kinds.mockResolvedValue([wechatKind, telegramKind])
  api.connections.mockResolvedValue(existing)
  const wrapper = mount(ConnectionsView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show" class="dialog"><slot /></div>' },
    GatewayDevices: true, GatewayEnrollment: true, RouterLink: true,
  } } })
  wrappers.push(wrapper)
  await tick()
  return wrapper
}
type Vm = { openConnection: (k: OpenClawConnectionKindInfo) => void; showConnectionModal: boolean }
async function open(wrapper: ReturnType<typeof mount>, kind = wechatKind) {
  (wrapper.vm as unknown as Vm).openConnection(kind)
  await tick()
}
function dialogButtons(wrapper: ReturnType<typeof mount>) { return wrapper.findAll('.dialog-actions button') }
function labels(wrapper: ReturnType<typeof mount>) { return dialogButtons(wrapper).map(b => b.text()) }
function button(wrapper: ReturnType<typeof mount>, label: string) {
  const found = dialogButtons(wrapper).find(b => b.text() === label)
  if (!found) throw new Error(`no button ${label}; have ${labels(wrapper).join(' | ')}`)
  return found
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  api.qr.mockImplementation(async (url: string) => `data:image/png;base64,${encodeURIComponent(url)}`)
})
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers() })

describe('WeChat connect dialog', () => {
  it('renders a QR code of the exact setup_url and re-renders when it changes', async () => {
    const wrapper = await render([conn({ setup_url: URL_A, setup_key: 'k1' })])
    api.complete.mockImplementation(() => new Promise(() => {}))
    await open(wrapper)
    expect(api.qr).toHaveBeenCalledWith(URL_A, expect.objectContaining({ width: 200 }))
    const img = wrapper.get('img.wechat-qr-img')
    expect(img.attributes('alt')).toBe('connections.wechatQrAlt')
    expect(img.attributes('src')).toContain('data:image/png')
    expect(wrapper.get('a.wechat-link').attributes('href')).toBe(URL_A)
    const first = img.attributes('src')

    api.complete.mockReset()
    api.complete.mockResolvedValue(conn({ setup_url: URL_B, setup_key: 'k2' }))
    ;(wrapper.vm as unknown as { connections: OpenClawConnection[] }).connections.splice(0, 1, conn({ setup_url: URL_B, setup_key: 'k1' }))
    await tick()
    expect(api.qr).toHaveBeenCalledWith(URL_B, expect.anything())
    expect(wrapper.get('img.wechat-qr-img').attributes('src')).not.toBe(first)
  })

  it('still shows the link when QR rendering fails', async () => {
    api.qr.mockRejectedValue(new Error('boom'))
    const wrapper = await render([conn({ setup_url: URL_A, setup_key: 'k1' })])
    api.complete.mockImplementation(() => new Promise(() => {}))
    await open(wrapper)
    expect(wrapper.find('img.wechat-qr-img').exists()).toBe(false)
    expect(wrapper.get('a.wechat-link').attributes('href')).toBe(URL_A)
  })

  it('shows setup_message whenever present', async () => {
    const wrapper = await render([conn({ setup_url: URL_A, setup_key: 'k1', setup_message: 'Scan me / 请扫码' })])
    api.complete.mockImplementation(() => new Promise(() => {}))
    await open(wrapper)
    expect(wrapper.text()).toContain('Scan me / 请扫码')
  })

  it('labels the primary button by state', async () => {
    // 1. no connection: Set up
    const wrapper = await render([])
    await open(wrapper)
    expect(labels(wrapper)).toEqual(['connections.close', 'connections.setUp'])

    // 2. QR active and auto-detect running: disabled "waiting", plus Get a new QR code
    const inFlight = deferred<OpenClawConnection>()
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockReturnValue(inFlight.promise)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(labels(wrapper)).toEqual(['connections.close', 'connections.wechatNewQr', 'connections.wechatWaiting', 'connections.removeConnection'])
    expect(button(wrapper, 'connections.wechatWaiting').attributes('disabled')).toBeDefined()

    // 3. key but auto-detect stopped (error): "I scanned the QR code"
    api.complete.mockReset()
    api.complete.mockRejectedValueOnce(new Error('network'))
    ;(wrapper.vm as unknown as Vm).showConnectionModal = false
    inFlight.resolve(conn({ setup_url: URL_A, setup_key: 'k1' }))
    await tick()
    await open(wrapper)
    await tick()
    expect(labels(wrapper)).toContain('connections.checkWeChatLogin')
    expect(button(wrapper, 'connections.checkWeChatLogin').attributes('disabled')).toBeUndefined()
  })

  it('shows Set up (no new-QR button) for a connection without setup_key', async () => {
    const wrapper = await render([conn()])
    await open(wrapper)
    expect(labels(wrapper)).toContain('connections.setUp')
    expect(labels(wrapper)).not.toContain('connections.checkWeChatLogin')
    expect(labels(wrapper)).not.toContain('connections.wechatNewQr')
    expect(api.complete).not.toHaveBeenCalled()
  })

  it('detects the scan automatically: pending, new QR, connected, then stops', async () => {
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete
      .mockResolvedValueOnce(conn({ setup_url: URL_A, setup_key: 'k1', setup_message: 'waiting' }))
      .mockResolvedValueOnce(conn({ setup_url: URL_B, setup_key: 'k2' }))
      .mockResolvedValueOnce(conn({ status: 'connected' }))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(api.create).toHaveBeenCalledWith(7, 'wechat', '', '')
    expect(api.complete).toHaveBeenCalledTimes(1)
    expect(api.complete).toHaveBeenLastCalledWith(7, 5, 'k1')

    await tick(1999)
    expect(api.complete).toHaveBeenCalledTimes(1) // 2 s gap respected
    await tick(1)
    expect(api.complete).toHaveBeenCalledTimes(2)
    expect(api.qr).toHaveBeenCalledWith(URL_B, expect.anything())
    expect(wrapper.get('a.wechat-link').attributes('href')).toBe(URL_B)

    await tick(2000)
    expect(api.complete).toHaveBeenCalledTimes(3)
    expect(api.complete).toHaveBeenLastCalledWith(7, 5, 'k2')
    expect(wrapper.text()).toContain('connections.wechatConnected')
    expect(wrapper.find('img.wechat-qr-img').exists()).toBe(false)

    await tick(60000)
    expect(api.complete).toHaveBeenCalledTimes(3)
  })

  it('stops checking when the dialog closes', async () => {
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(api.complete).toHaveBeenCalledTimes(1)
    await button(wrapper, 'connections.close').trigger('click')
    await tick(30000)
    expect(api.complete).toHaveBeenCalledTimes(1)
  })

  it('stops checking when the component unmounts', async () => {
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    wrapper.unmount()
    await tick(30000)
    expect(api.complete).toHaveBeenCalledTimes(1)
  })

  it('keeps at most one check in flight', async () => {
    const pending = deferred<OpenClawConnection>()
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockReturnValue(pending.promise)
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick(60000)
    expect(api.complete).toHaveBeenCalledTimes(1)
    api.complete.mockResolvedValue(conn({ status: 'connected' }))
    pending.resolve(conn({ setup_url: URL_A, setup_key: 'k1' }))
    await tick(2000)
    expect(api.complete).toHaveBeenCalledTimes(2)
  })

  it('ignores a stale response after "Get a new QR code" and does not overlap requests', async () => {
    const stale = deferred<OpenClawConnection>()
    api.create.mockResolvedValueOnce(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockReturnValueOnce(stale.promise)
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(api.complete).toHaveBeenCalledTimes(1)

    api.create.mockResolvedValueOnce(conn({ setup_url: URL_B, setup_key: 'k2' }))
    api.complete.mockResolvedValue(conn({ setup_url: URL_B, setup_key: 'k2' }))
    await button(wrapper, 'connections.wechatNewQr').trigger('click')
    await tick()
    expect(api.create).toHaveBeenCalledTimes(2)
    expect(api.complete).toHaveBeenCalledTimes(1) // new run waits for the old request to settle

    // The old request finally returns "connected" for the old key: must be ignored.
    stale.resolve(conn({ status: 'connected' }))
    await tick()
    expect(wrapper.text()).not.toContain('connections.wechatConnected')
    expect(wrapper.get('a.wechat-link').attributes('href')).toBe(URL_B)
    expect(api.complete).toHaveBeenCalledTimes(2)
    expect(api.complete).toHaveBeenLastCalledWith(7, 5, 'k2')
  })

  it('shows the expiry message six minutes after the QR was first shown', async () => {
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick(5 * 60 * 1000)
    expect(wrapper.text()).not.toContain('connections.wechatQrExpired')
    await tick(61 * 1000)
    expect(wrapper.text()).toContain('connections.wechatQrExpired')
    const calls = api.complete.mock.calls.length
    await tick(60000)
    expect(api.complete).toHaveBeenCalledTimes(calls)
    expect(labels(wrapper)).toContain('connections.checkWeChatLogin')
    expect(labels(wrapper)).toContain('connections.wechatNewQr')

    // "I scanned the QR code" restarts auto-detect with a fresh window.
    await button(wrapper, 'connections.checkWeChatLogin').trigger('click')
    await tick()
    expect(api.complete).toHaveBeenCalledTimes(calls + 1)
    expect(wrapper.text()).not.toContain('connections.wechatQrExpired')
  })

  it('shows the error and stops when a check fails', async () => {
    api.create.mockResolvedValue(conn({ setup_url: URL_A, setup_key: 'k1' }))
    api.complete.mockRejectedValue(new Error('worker unavailable'))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(wrapper.get('[role="alert"].field-error').text()).toBe('worker unavailable')
    await tick(30000)
    expect(api.complete).toHaveBeenCalledTimes(1)
    expect(labels(wrapper)).toContain('connections.checkWeChatLogin')
  })

  it('shows the error when starting a QR fails', async () => {
    api.create.mockRejectedValue(new Error('no bridge'))
    const wrapper = await render([])
    await open(wrapper)
    await button(wrapper, 'connections.setUp').trigger('click')
    await tick()
    expect(wrapper.get('[role="alert"].field-error').text()).toBe('no bridge')
    expect(api.complete).not.toHaveBeenCalled()
  })

  it('keeps the QR code and its timer when a check returns the same session without a link', async () => {
    const wrapper = await render([conn({ setup_url: URL_A, setup_key: 'k1', setup_message: 'scan me' })])
    api.complete.mockResolvedValue(conn({ status: 'pending', setup_url: '', setup_key: 'k1', setup_message: '' }))
    await open(wrapper)
    await tick(1)
    expect(api.complete).toHaveBeenCalled()
    expect(wrapper.get('img.wechat-qr-img').exists()).toBe(true)
    expect(api.qr).toHaveBeenCalledTimes(1)
    await tick(5 * 60 * 1000)
    expect(wrapper.text()).not.toContain('connections.wechatQrExpired')
    await tick(61 * 1000)
    expect(wrapper.text()).toContain('connections.wechatQrExpired')
  })

  it('leaves Telegram unchanged', async () => {
    api.create.mockResolvedValue(conn({ id: 9, kind: 'telegram', status: 'connected' }))
    const wrapper = await render([])
    await open(wrapper, telegramKind)
    expect(wrapper.find('img.wechat-qr-img').exists()).toBe(false)
    expect(labels(wrapper)).toEqual(['connections.close', 'connections.connectTelegram'])
    expect(button(wrapper, 'connections.connectTelegram').attributes('disabled')).toBeDefined()
    await wrapper.get('#telegram-token').setValue('123:abc')
    await button(wrapper, 'connections.connectTelegram').trigger('click')
    await tick(10000)
    expect(api.create).toHaveBeenCalledWith(7, 'telegram', '', '123:abc')
    expect(api.complete).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('connections.setupPrepared')
  })
})
