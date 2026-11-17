import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import type { OpenClawAPIKey, OpenClawDashboard, OpenClawInstance } from '@/api/openclaw'
import en from '@/i18n/locales/en/connections'
import zh from '@/i18n/locales/zh/connections'
import ConnectionsView from '../ConnectionsView.vue'

const api = vi.hoisted(() => ({ lifecycle: vi.fn(), dashboard: vi.fn(), kinds: vi.fn(), connections: vi.fn() }))
vi.mock('@/api/openclaw', () => ({
  getOpenClawLifecycle: api.lifecycle, getOpenClawDashboard: api.dashboard,
  listOpenClawConnectionKinds: api.kinds, listOpenClawConnections: api.connections,
  provisionOpenClawInstance: vi.fn(), deleteOpenClawConnection: vi.fn(), deleteOpenClawInstance: vi.fn(),
  createOpenClawConnection: vi.fn(), completeOpenClawConnection: vi.fn(), createOpenClawAccessRequest: vi.fn(),
  createOpenClawInstance: vi.fn(), getOpenClawGatewayLaunch: vi.fn(), renameOpenClawInstance: vi.fn(),
  startOpenClawInstance: vi.fn(), stopOpenClawInstance: vi.fn(),
}))
vi.mock('@/composables/useTelegramAvailability', () => ({ useTelegramAvailability: () => ({ telegramAvailable: false }) }))

const instance: OpenClawInstance = { id: 7, tenant_slug: 'u42-0123456789ab', name: 'Workspace', status: 'running', worker_host: 'default', created_at: '2026-11-01T01:00:00Z', updated_at: '2026-11-01T02:00:00Z' }
const billingKey: OpenClawAPIKey = { id: 99, name: 'OpenClaw · Workspace', status: 'active', quota: 0, quota_used: 0, usage_count: 42, usage_cost: 1.5 }
const wrappers: ReturnType<typeof mount>[] = []

async function render(dashboard: OpenClawDashboard, locale: 'en' | 'zh' = 'en') {
  api.dashboard.mockResolvedValue(dashboard)
  const i18n = createI18n({ legacy: false, locale, messages: { en, zh }, missingWarn: false, fallbackWarn: false })
  const wrapper = mount(ConnectionsView, { global: { plugins: [i18n], stubs: {
    AppLayout: { template: '<main><slot /></main>' }, BaseDialog: true, GatewayDevices: true, GatewayEnrollment: true, RouterLink: true,
  } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper.get('[data-testid="last-request-metric"]')
}
function dashboard(apiKey?: OpenClawAPIKey): OpenClawDashboard {
  return { instance, fleet_state: 'running', checked_at: '2026-11-17T09:00:00Z', connection_count: 2, connected_count: 2, api_key: apiKey }
}

beforeEach(() => {
  vi.resetAllMocks()
  api.lifecycle.mockResolvedValue({ instance, can_create: false, can_delete: true, self_service_enabled: true })
  api.kinds.mockResolvedValue([])
  api.connections.mockResolvedValue([])
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('Connections last model request card', () => {
  it('shows the billing key\'s newest billed request, not connection activity', async () => {
    const card = await render(dashboard({ ...billingKey, usage_count: 777, last_used_at: '2026-11-17T08:59:59Z', last_request_at: '2026-11-17T08:42:00Z' }))
    expect(card.get('span').text()).toBe('Last model request')
    expect(card.get('small').text()).toBe('From the workspace billing key; not per connection')
    expect(card.get('time').attributes('datetime')).toBe('2026-11-17T08:42:00Z')
    expect(card.text()).not.toMatch(/connection activity/i)
    expect(card.text()).not.toContain('777')
  })

  it('says no requests yet when the billing key has never been billed', async () => {
    const card = await render(dashboard({ ...billingKey, usage_count: 0, usage_cost: 0, last_used_at: '2026-11-17T08:59:59Z' }))
    expect(card.find('time').exists()).toBe(false)
    expect(card.get('strong').text()).toBe('No requests yet')
  })

  it('does not claim zero requests when the billing key is unavailable', async () => {
    const card = await render(dashboard())
    expect(card.find('time').exists()).toBe(false)
    expect(card.get('strong').text()).toBe('—')
  })

  it('ignores an unparseable timestamp instead of rendering Invalid Date', async () => {
    const card = await render(dashboard({ ...billingKey, last_request_at: 'not-a-date' }))
    expect(card.find('time').exists()).toBe(false)
    expect(card.get('strong').text()).toBe('No requests yet')
  })

  it('uses matching Chinese copy', async () => {
    const card = await render(dashboard({ ...billingKey, last_request_at: '2026-11-17T08:42:00Z' }), 'zh')
    expect(card.get('span').text()).toBe('最近模型请求')
    expect(card.get('small').text()).toBe('来自工作区计费密钥，不区分具体连接')
    expect(card.text()).not.toContain('连接活动')
  })
})
