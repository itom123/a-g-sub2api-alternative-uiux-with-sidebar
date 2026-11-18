import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import type { OpenClawAdminInstance } from '@/api/openclaw'
import { currencyRate } from '@/utils/currency'
import OpenClawFleetView from '../OpenClawFleetView.vue'

const { listInstances, listRequests } = vi.hoisted(() => ({
  listInstances: vi.fn(),
  listRequests: vi.fn(),
}))

vi.mock('@/api/openclaw', () => ({
  listAdminOpenClawInstances: listInstances,
  listAdminOpenClawAccessRequests: listRequests,
  deleteAdminOpenClawInstance: vi.fn(),
  reconcileAdminOpenClawInstance: vi.fn(),
  reviewAdminOpenClawAccessRequest: vi.fn(),
  setAdminOpenClawAccessPolicy: vi.fn(),
}))

const SelectStub = defineComponent({
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: `<select data-test="sort" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
    <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
  </select>`,
})

function connection(kind: string, title: string, status: string, label = '') {
  return { kind, title, status, label, updated_at: '2026-11-10T00:00:00Z' }
}

function workspace(overrides: Partial<OpenClawAdminInstance> = {}): OpenClawAdminInstance {
  return {
    id: 1,
    tenant_slug: 'u1-idle',
    name: 'Idle workspace',
    status: 'running',
    worker_host: 'default',
    created_at: '2026-11-10T00:00:00Z',
    updated_at: '2026-11-10T00:00:00Z',
    user_id: 1,
    user_email: 'idle@example.test',
    billing_group: 'openai-claw',
    billing_rate: 0.3,
    api_key_status: 'active',
    allowed_models: ['gpt-5.6-sol'],
    default_model: 'gpt-5.6-sol',
    models_source: 'group',
    mapping_enabled: true,
    model_sync: 'synced',
    connections: [connection('wechat', 'WeChat', 'pending')],
    connection_count: 1,
    connected_count: 0,
    request_count: 0,
    usage_cost: 0,
    token_count: 0,
    requests_7d: 0,
    usage_cost_7d: 0,
    ...overrides,
  }
}

const busy = workspace({
  id: 2,
  tenant_slug: 'u2-busy',
  name: 'Busy workspace',
  user_id: 2,
  user_email: 'busy@example.test',
  connections: [connection('wechat', 'WeChat', 'pending'), connection('telegram', 'Telegram', 'connected', 'Support bot')],
  connection_count: 2,
  connected_count: 1,
  request_count: 1284,
  usage_cost: 51.9634,
  token_count: 12_000_000,
  requests_7d: 1,
  usage_cost_7d: 2,
  last_request_at: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
})

async function mountFleet() {
  const wrapper = mount(OpenClawFleetView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Select: SelectStub } },
  })
  await flushPromises()
  return wrapper
}

function usageCells(wrapper: Awaited<ReturnType<typeof mountFleet>>) {
  return wrapper.findAll('td[data-label="Usage"]').map(cell => cell.text().replace(/\s+/g, ' '))
}

describe('OpenClaw fleet usage', () => {
  beforeEach(() => {
    currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
    // The API lists newest workspaces first: the idle one, then the busy one.
    listInstances.mockResolvedValue([workspace(), busy])
    listRequests.mockResolvedValue([])
  })

  afterEach(() => {
    currencyRate.value = null
    vi.clearAllMocks()
  })

  it('shows ledger requests beside spend instead of a zero call count', async () => {
    const wrapper = await mountFleet()
    const [idle, active] = usageCells(wrapper)

    expect(active).toContain('¥363.74 billed')
    expect(active).toContain('1,284 requests · 12.0M tokens')
    expect(active).toContain('Last 7 days: 1 request · ¥14.00')
    expect(active).toContain('Last call')
    expect(wrapper.find('td[data-label="Usage"] time').attributes('datetime')).toBe(busy.last_request_at)
    expect(idle).toBe('No API calls yet')
    expect(wrapper.text()).not.toMatch(/\b0 calls\b/)
  })

  it('summarises fleet requests and spend from the same ledger', async () => {
    const wrapper = await mountFleet()
    const cards = wrapper.findAll('article').map(card => card.findAll('p').map(line => line.text()).join(' | '))

    expect(cards).toEqual([
      'Workspaces | 2 | 2 running',
      'Connected services | 1 | 2 set-ups not finished',
      'API requests | 1,284 | 1 in the last 7 days',
      'Billed | ¥363.74 | ¥14.00 in the last 7 days',
    ])
  })

  it('counts only finished set-ups in workspaces that still serve customers', async () => {
    const queued = workspace({
      id: 3,
      tenant_slug: 'u3-gone',
      name: 'Queued workspace',
      deletion_scheduled_at: '2026-11-20T00:00:00Z',
      connections: [connection('telegram', 'Telegram', 'connected'), connection('gws', 'Google Workspace', 'pending')],
      connection_count: 2,
      connected_count: 1,
    })
    const finished = workspace({ id: 4, tenant_slug: 'u4-done', name: 'Finished', connections: [connection('telegram', 'Telegram', 'connected')] })
    listInstances.mockResolvedValue([queued, finished])
    const wrapper = await mountFleet()
    const card = wrapper.findAll('article')[1].findAll('p').map(line => line.text()).join(' | ')
    expect(card).toBe('Connected services | 1 | All set-ups finished')

    listInstances.mockResolvedValue([workspace({ connections: [] })])
    const empty = await mountFleet()
    expect(empty.findAll('article')[1].text()).toContain('No services set up yet')
  })

  it('lists every service of a workspace with its set-up state', async () => {
    const wrapper = await mountFleet()
    const cells = wrapper.findAll('td[data-label="Services"]').map(cell => cell.findAll('li').map(item => item.findAll('span').map(part => part.text()).join(' ')))
    expect(cells[0]).toEqual(['WeChat Set-up not finished'])
    expect(cells[1]).toEqual(['WeChat Set-up not finished', 'Telegram · Support bot Connected'])

    listInstances.mockResolvedValue([workspace({ connections: [] })])
    const empty = await mountFleet()
    expect(empty.find('td[data-label="Services"]').text()).toBe('No services')
  })

  it('shows the billing group\'s models and whether the workspace holds them', async () => {
    listInstances.mockResolvedValue([
      workspace({ allowed_models: ['gpt-6.2-sol', 'gpt-5.6-sol'], default_model: 'gpt-6.2-sol', model_sync: 'pending' }),
      workspace({ id: 5, models_source: 'defaults', mapping_enabled: false, model_sync: undefined }),
      workspace({ id: 6, mapping_enabled: false, mapping_error: 'OPENCLAW_MODEL_GATEWAY_BASE_URL must be an http(s) URL', model_sync: undefined }),
      workspace({ id: 7, model_sync: 'failed' }),
      workspace({ id: 8, models_source: 'fallback' }),
    ])
    const wrapper = await mountFleet()
    const [pending, defaults, broken, failed, fallback] = wrapper.findAll('td[data-label="Models"]').map(cell => [
      ...cell.findAll('li').map(item => item.text()),
      ...cell.findAll('td > div').map(line => line.text()),
    ].join(' | '))
    expect(pending).toBe("gpt-6.2-sol | gpt-5.6-sol | Default: gpt-6.2-sol | From the openai-claw group's model allowlist | Updating the workspace…")
    expect(defaults).toContain('Built-in defaults: the group allows every model')
    expect(defaults).toContain('Not sent to the workspace: model mapping is off')
    expect(broken).toContain('Model mapping error: OPENCLAW_MODEL_GATEWAY_BASE_URL must be an http(s) URL')
    expect(failed).toContain('Workspace update failed; retrying')
    expect(fallback).toContain("Built-in defaults: the openai-claw group's allowlist names no model a workspace can offer")
    expect(wrapper.text()).not.toContain('Mapping not configured')
  })

  it('sorts workspaces by spend so heavy users surface first', async () => {
    const wrapper = await mountFleet()
    const names = () => wrapper.findAll('td[data-label="Workspace"] .font-medium').map(cell => cell.text())
    expect(names()).toEqual(['Idle workspace', 'Busy workspace'])

    await wrapper.find('[data-test="sort"]').setValue('spend')
    expect(names()).toEqual(['Busy workspace', 'Idle workspace'])
  })
})
