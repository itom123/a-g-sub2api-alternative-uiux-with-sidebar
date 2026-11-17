import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OpenClawConnection, OpenClawConnectionKindInfo, OpenClawInstance } from '@/api/openclaw'
import ConnectionsView from '../ConnectionsView.vue'
import { WECHAT_QR_OPTIONS } from '../wechatQr'

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

const START_URL = 'https://t.me/testbugbot?start=ABCDEFGHJK'
const KEY = 'tgclaim_' + 'a'.repeat(32)
const START_MSG = 'Open @testbugbot in Telegram and tap Start / 在 Telegram 打开'
const telegramKind: OpenClawConnectionKindInfo = { kind: 'telegram', title: 'Telegram', setup_available: true } as OpenClawConnectionKindInfo

function conn(over: Partial<OpenClawConnection> = {}): OpenClawConnection {
  return { id: 9, instance_id: 7, kind: 'telegram', status: 'pending', label: '', usage_count: 0, created_at: '2026-11-17T01:00:00Z', updated_at: '2026-11-17T01:00:00Z', ...over }
}
function pending(over: Partial<OpenClawConnection> = {}) {
  return conn({ setup_url: START_URL, setup_key: KEY, setup_message: START_MSG, ...over })
}

const wrappers: ReturnType<typeof mount>[] = []
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms) }
async function render(existing: OpenClawConnection[] = []) {
  const instance: OpenClawInstance = { id: 7, tenant_slug: 'u42', name: 'Workspace', status: 'running', worker_host: 'default', created_at: '2026-11-01T01:00:00Z', updated_at: '2026-11-01T02:00:00Z' }
  api.lifecycle.mockResolvedValue({ instance, can_create: false, can_delete: true, self_service_enabled: true })
  api.dashboard.mockResolvedValue({ instance, fleet_state: 'running', checked_at: '2026-11-01T03:00:00Z', gateway_enabled: true, connection_count: 0, connected_count: 0, usage_count: 0 })
  api.kinds.mockResolvedValue([telegramKind])
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
async function open(wrapper: ReturnType<typeof mount>) {
  (wrapper.vm as unknown as Vm).openConnection(telegramKind)
  await tick()
}
function labels(wrapper: ReturnType<typeof mount>) { return wrapper.findAll('.dialog-actions button').map(b => b.text()) }
async function submitToken(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('#telegram-token').setValue('123456789:AAH-token')
  const connect = wrapper.findAll('.dialog-actions button').find(b => b.text() === 'connections.connectTelegram')
  if (!connect) throw new Error('no connect button: ' + labels(wrapper).join(' | '))
  await connect.trigger('click')
  await tick()
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  api.qr.mockImplementation(async (url: string) => `data:image/png;base64,${encodeURIComponent(url)}`)
})
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers() })

describe('Telegram owner claim', () => {
  it('shows the Start link, QR code and code, then reports the bound owner', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValueOnce(pending()).mockResolvedValueOnce(conn({ status: 'connected', setup_message: 'Connected: @testbugbot now answers only Lady @ladyblue.' }))
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    expect(api.create).toHaveBeenCalledWith(7, 'telegram', '', '123456789:AAH-token')

    const box = wrapper.get('[data-testid="telegram-claim"]')
    expect(box.get('[data-testid="telegram-open-bot"]').attributes('href')).toBe(START_URL)
    expect(api.qr).toHaveBeenCalledWith(START_URL, WECHAT_QR_OPTIONS)
    expect(box.get('img.telegram-qr-img').attributes('alt')).toBe('connections.telegramQrAlt')
    expect(box.get('[data-testid="telegram-claim-code"]').text()).toBe('ABCDE-FGHJK')
    expect(box.find('[data-testid="telegram-waiting"]').exists()).toBe(true)
    expect(wrapper.find('#telegram-token').exists()).toBe(false)
    expect(labels(wrapper)).not.toContain('connections.connectTelegram')
    expect(wrapper.text()).not.toContain(START_MSG)

    await tick(2500)
    expect(api.complete).toHaveBeenLastCalledWith(7, 9, KEY)
    expect(wrapper.find('[data-testid="telegram-notice"]').exists()).toBe(false)
    await tick(2500)
    expect(api.complete).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="telegram-owner"]').text()).toContain('Lady @ladyblue')
    expect(wrapper.find('[data-testid="telegram-claim"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('connections.setupPrepared')
    await tick(10000)
    expect(api.complete).toHaveBeenCalledTimes(2)
  })

  it('shows worker notices other than the standard prompt and keeps waiting', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValueOnce(pending({ setup_message: 'This bot is being read by another program.' })).mockImplementation(() => new Promise(() => {}))
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    await tick(2500)
    expect(wrapper.get('[data-testid="telegram-notice"]').text()).toBe('This bot is being read by another program.')
    expect(wrapper.find('[data-testid="telegram-claim"]').exists()).toBe(true)
    await tick(2500)
    expect(api.complete).toHaveBeenCalledTimes(2)
  })

  it('returns to the token form when the worker reports the attempt expired', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValue(conn({ setup_message: 'This connection attempt expired.' }))
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    await tick(2500)
    expect(wrapper.find('[data-testid="telegram-claim"]').exists()).toBe(false)
    expect(wrapper.find('#telegram-token').exists()).toBe(true)
    expect(wrapper.get('[data-testid="telegram-notice"]').text()).toBe('This connection attempt expired.')
    await tick(10000)
    expect(api.complete).toHaveBeenCalledTimes(1)
  })

  it('stops after 15 minutes and asks for the token again', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValue(pending())
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    await tick(14 * 60 * 1000)
    expect(wrapper.text()).not.toContain('connections.telegramClaimExpired')
    await tick(61 * 1000)
    expect(wrapper.text()).toContain('connections.telegramClaimExpired')
    expect(wrapper.find('#telegram-token').exists()).toBe(true)
    const calls = api.complete.mock.calls.length
    await tick(10000)
    expect(api.complete).toHaveBeenCalledTimes(calls)
  })

  it('lets the customer start over with another token', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValue(pending())
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    await wrapper.get('[data-testid="telegram-restart"]').trigger('click')
    await tick()
    expect(wrapper.find('#telegram-token').exists()).toBe(true)
    expect(labels(wrapper)).toContain('connections.connectTelegram')
    await tick(10000)
    expect(api.complete).not.toHaveBeenCalled()
  })

  it('stops polling when the dialog closes', async () => {
    api.create.mockResolvedValue(pending())
    api.complete.mockResolvedValue(pending())
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    ;(wrapper.vm as unknown as Vm).showConnectionModal = false
    await tick(10000)
    expect(api.complete).not.toHaveBeenCalled()
  })

  it('keeps the old behaviour when the worker connects at once', async () => {
    api.create.mockResolvedValue(conn({ status: 'connected' }))
    const wrapper = await render([])
    await open(wrapper)
    await submitToken(wrapper)
    await tick(10000)
    expect(wrapper.find('[data-testid="telegram-claim"]').exists()).toBe(false)
    expect(api.complete).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('connections.setupPrepared')
  })
})
