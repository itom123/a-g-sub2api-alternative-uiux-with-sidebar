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
    mapping_enabled: true,
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
      'Connected services | 1 | 3 configured',
      'API requests | 1,284 | 1 in the last 7 days',
      'Billed | ¥363.74 | ¥14.00 in the last 7 days',
    ])
  })

  it('sorts workspaces by spend so heavy users surface first', async () => {
    const wrapper = await mountFleet()
    const names = () => wrapper.findAll('td[data-label="Workspace"] .font-medium').map(cell => cell.text())
    expect(names()).toEqual(['Idle workspace', 'Busy workspace'])

    await wrapper.find('[data-test="sort"]').setValue('spend')
    expect(names()).toEqual(['Busy workspace', 'Idle workspace'])
  })
})
