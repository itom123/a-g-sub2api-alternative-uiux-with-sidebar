import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import type { Group, UserSubscription } from '@/types'
import type { Column } from '@/components/common/types'
import { currencyRate } from '@/utils/currency'
import SubscriptionsView from '../SubscriptionsView.vue'

const { listSubscriptions, getAllGroups } = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: { list: listSubscriptions },
    groups: { getAll: getAllGroups }
  }
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params: Record<string, unknown> = {}) => {
      const messages: Record<string, string> = {
        'common.notAvailable': 'N/A',
        'admin.subscriptions.daily': 'Daily',
        'admin.subscriptions.weekly': 'Weekly',
        'admin.subscriptions.monthly': 'Monthly',
        'admin.subscriptions.unlimited': 'Unlimited',
        'admin.subscriptions.windowNotActive': 'Window not active',
        'admin.subscriptions.resetInMinutes': 'Resets in {minutes}m',
        'admin.subscriptions.resetInHoursMinutes': 'Resets in {hours}h {minutes}m',
        'admin.subscriptions.resetInDaysHours': 'Resets in {days}d {hours}h',
        'admin.subscriptions.quotaEndsInMinutes': 'Quota ends in {minutes}m',
        'admin.subscriptions.quotaEndsInHoursMinutes': 'Quota ends in {hours}h {minutes}m',
        'admin.subscriptions.quotaEndsInDaysHours': 'Quota ends in {days}d {hours}h'
      }
      return (messages[key] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name]))
    }
  })
}))

const DataTableStub = defineComponent({
  name: 'DataTable',
  props: ['columns', 'data', 'selectedKeys'],
  emits: ['sort', 'update:selectedKeys'],
  template: `
    <table>
      <thead><tr><th v-for="column in columns" :key="column.key" :data-column="column.key">{{ column.label }}</th></tr></thead>
      <tbody><tr v-for="row in data" :key="row.id" :data-subscription="row.id">
        <td v-for="column in columns" :key="column.key" :data-column="column.key">
          <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]" />
        </td>
      </tr></tbody>
    </table>
  `
})

const PERIODS = ['daily', 'weekly', 'monthly'] as const
const USAGE_KEYS = ['daily_usage', 'weekly_usage', 'monthly_usage']
const HIDDEN_COLUMNS_KEY = 'subscription-hidden-columns'

function subscription(overrides: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 9,
    user_id: 42,
    group_id: 3,
    status: 'active',
    starts_at: '2026-10-01T00:00:00Z',
    expires_at: '2026-12-01T00:00:00Z',
    daily_usage_usd: 0,
    weekly_usage_usd: 8,
    monthly_usage_usd: 1000,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: '',
    updated_at: '',
    group: { name: 'Plan', daily_limit_usd: 10, weekly_limit_usd: 100, monthly_limit_usd: 1000 } as Group,
    ...overrides
  }
}

function mountView() {
  return mount(SubscriptionsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true,
        Teleport: true,
        RouterLink: true
      }
    }
  })
}

type ViewWrapper = ReturnType<typeof mountView>
const wrappers: ViewWrapper[] = []

async function renderRows(rows: UserSubscription[] = [subscription()]) {
  listSubscriptions.mockResolvedValue({ items: rows, total: rows.length, pages: 1 })
  const wrapper = mountView()
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

function usageCell(wrapper: ViewWrapper, period: typeof PERIODS[number], id = 9) {
  return wrapper.get(`[data-subscription="${id}"] [data-column="${period}_usage"] .subscription-usage-cell`)
}

async function openColumnSettings(wrapper: ViewWrapper) {
  await wrapper.get('button[title="admin.users.columnSettings"]').trigger('click')
}

async function togglePeriod(wrapper: ViewWrapper, label: string) {
  await wrapper.findAll('button').find(button => button.text() === label)!.trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-01T12:00:00Z'))
  localStorage.clear()
  getAllGroups.mockResolvedValue([])
  currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  currencyRate.value = null
  vi.useRealTimers()
})

describe('admin subscription usage columns', () => {
  it('renders three equal-width nonsortable headers and independent slots without repeated period labels', async () => {
    const wrapper = await renderRows()
    const table = wrapper.getComponent(DataTableStub)
    const columns = table.props('columns') as Column[]
    const usageColumns = columns.filter(column => USAGE_KEYS.includes(column.key))

    expect(columns.map(column => column.key)).toEqual([
      'user', 'group', ...USAGE_KEYS, 'expires_at', 'status', 'actions'
    ])
    expect(usageColumns.map(column => ({
      key: column.key, width: column.width, minWidth: column.minWidth,
      sortable: column.sortable, class: column.class, mobileLayout: column.mobileLayout
    }))).toEqual(USAGE_KEYS.map(key => ({
      key, width: 190, minWidth: 190, sortable: false, class: 'text-right', mobileLayout: 'stacked'
    })))
    expect(columns.find(column => column.key === 'user')?.width).toBeUndefined()
    expect(columns.filter(column => column.sortable).map(column => column.key)).toEqual(['expires_at', 'status'])
    expect(table.vm.$slots['cell-usage']).toBeUndefined()
    for (const [index, period] of PERIODS.entries()) {
      expect(wrapper.get(`th[data-column="${period}_usage"]`).text()).toBe(['Daily', 'Weekly', 'Monthly'][index])
      expect(table.vm.$slots[`cell-${period}_usage`]).toBeTypeOf('function')
      expect(usageCell(wrapper, period).attributes('data-period')).toBe(period)
      expect(usageCell(wrapper, period).text()).not.toMatch(/Daily|Weekly|Monthly/)
    }
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('¥0.00')
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-limit"]').text()).toBe('¥70.00')
    expect(usageCell(wrapper, 'weekly').get('[data-test="usage-used"]').text()).toBe('¥56.00')
    expect(usageCell(wrapper, 'monthly').get('[data-test="usage-used"]').text()).toBe('¥7000.00')
    expect(table.props('selectedKeys')).toEqual([])
    expect(listSubscriptions).toHaveBeenCalledWith(1, expect.any(Number), expect.any(Object), expect.any(Object))
  })

  it('preserves true zero and formats honest USD amounts when exchange rates are unavailable', async () => {
    currencyRate.value = null
    const wrapper = await renderRows([subscription({ daily_usage_usd: 0, weekly_usage_usd: 0.04 })])
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('$0.00')
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-limit"]').text()).toBe('$10.00')
    expect(usageCell(wrapper, 'weekly').get('[data-test="usage-used"]').text()).toBe('$0.04')
  })

  it('shows each explicit null or nonpositive allowance as unlimited while retaining its used amount', async () => {
    const wrapper = await renderRows([subscription({
      group: { name: 'Uncapped', daily_limit_usd: 0, weekly_limit_usd: -1, monthly_limit_usd: null } as Group
    })])
    for (const period of PERIODS) {
      expect(usageCell(wrapper, period).attributes('data-state')).toBe('unlimited')
      expect(usageCell(wrapper, period).get('[data-test="usage-limit"]').text()).toBe('Unlimited')
    }
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('¥0.00')
    expect(usageCell(wrapper, 'monthly').get('[data-test="usage-used"]').text()).toBe('¥7000.00')
  })

  it('keeps a missing group unknown rather than fabricating unlimited allowances', async () => {
    const wrapper = await renderRows([subscription({ group: undefined })])
    for (const period of PERIODS) {
      expect(usageCell(wrapper, period).attributes('data-state')).toBe('unknown')
      expect(usageCell(wrapper, period).get('[data-test="usage-limit"]').text()).toBe('N/A')
      expect(usageCell(wrapper, period).text()).not.toContain('Unlimited')
    }
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('¥0.00')
  })

  it('does not turn missing period data into zero or unlimited', async () => {
    const wrapper = await renderRows([subscription({
      daily_usage_usd: undefined as unknown as number,
      group: { name: 'Partial plan', daily_limit_usd: 10, weekly_limit_usd: undefined, monthly_limit_usd: 1000 } as unknown as Group
    })])
    expect(usageCell(wrapper, 'daily').attributes('data-state')).toBe('unknown')
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('N/A')
    expect(usageCell(wrapper, 'weekly').attributes('data-state')).toBe('unknown')
    expect(usageCell(wrapper, 'weekly').get('[data-test="usage-limit"]').text()).toBe('N/A')
    expect(usageCell(wrapper, 'monthly').attributes('data-state')).toBe('finite')
  })

  it('retains over-limit usage amounts instead of clamping them to the limit', async () => {
    const wrapper = await renderRows([subscription({ daily_usage_usd: 12.5 })])
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-used"]').text()).toBe('¥87.50')
    expect(usageCell(wrapper, 'daily').get('[data-test="usage-limit"]').text()).toBe('¥70.00')
  })

  it('reserves the common subline without inventing reset text for absent windows', async () => {
    const wrapper = await renderRows()
    for (const period of PERIODS) {
      const cell = usageCell(wrapper, period)
      expect(cell.get('.subscription-usage-cell__window').text()).toBe('')
      expect(cell.find('.subscription-usage-cell__reset').exists()).toBe(false)
      expect(cell.text()).not.toContain('Window not active')
    }
  })

  it('preserves the existing one-day, seven-day, and thirty-day reset calculations', async () => {
    const start = '2026-11-01T00:00:00Z'
    const wrapper = await renderRows([subscription({
      daily_window_start: start, weekly_window_start: start, monthly_window_start: start
    })])
    expect(usageCell(wrapper, 'daily').get('.subscription-usage-cell__reset').text()).toBe('Resets in 12h 0m')
    expect(usageCell(wrapper, 'weekly').get('.subscription-usage-cell__reset').text()).toBe('Resets in 6d 12h')
    expect(usageCell(wrapper, 'monthly').get('.subscription-usage-cell__reset').text()).toBe('Resets in 29d 12h')
  })

  it('uses the subscription expiry for one-time daily wording, not its rolling daily window', async () => {
    const wrapper = await renderRows([subscription({
      starts_at: '2026-11-01T00:00:00Z',
      expires_at: '2026-11-02T00:00:00Z',
      daily_window_start: '2026-10-31T00:00:00Z',
      weekly_window_start: '2026-11-01T00:00:00Z'
    })])
    expect(usageCell(wrapper, 'daily').get('.subscription-usage-cell__reset').text()).toBe('Quota ends in 12h 0m')
    expect(usageCell(wrapper, 'daily').text()).not.toContain('Resets')
    expect(usageCell(wrapper, 'weekly').get('.subscription-usage-cell__reset').text()).toBe('Resets in 6d 12h')
  })

  it('keeps one-time daily expiry text absent when the daily window has not started', async () => {
    const wrapper = await renderRows([subscription({
      starts_at: '2026-11-01T00:00:00Z', expires_at: '2026-11-02T00:00:00Z', daily_window_start: null
    })])
    expect(usageCell(wrapper, 'daily').find('.subscription-usage-cell__reset').exists()).toBe(false)
    expect(usageCell(wrapper, 'daily').get('.subscription-usage-cell__window').text()).toBe('')
  })

  it('keeps inactive wording for elapsed windows and expired one-time daily quotas', async () => {
    const wrapper = await renderRows([
      subscription({ daily_window_start: '2026-10-30T00:00:00Z' }),
      subscription({
        id: 10, starts_at: '2026-10-31T00:00:00Z', expires_at: '2026-11-01T00:00:00Z',
        daily_window_start: '2026-10-31T00:00:00Z'
      })
    ])
    expect(usageCell(wrapper, 'daily').get('.subscription-usage-cell__reset').text()).toBe('Window not active')
    expect(usageCell(wrapper, 'daily', 10).get('.subscription-usage-cell__reset').text()).toBe('Window not active')
  })

  it('migrates legacy hidden usage into all three saved keys without changing other hidden columns', async () => {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(['group', 'usage']))
    const wrapper = await renderRows()
    expect((wrapper.getComponent(DataTableStub).props('columns') as Column[]).map(column => column.key))
      .toEqual(['user', 'expires_at', 'status', 'actions'])
    expect(JSON.parse(localStorage.getItem(HIDDEN_COLUMNS_KEY)!)).toEqual(['group', ...USAGE_KEYS])
    await openColumnSettings(wrapper)
    await togglePeriod(wrapper, 'Weekly')
    expect(wrapper.find('th[data-column="weekly_usage"]').exists()).toBe(true)
    expect(wrapper.find('th[data-column="daily_usage"]').exists()).toBe(false)
    expect(wrapper.find('th[data-column="monthly_usage"]').exists()).toBe(false)
    expect(JSON.parse(localStorage.getItem(HIDDEN_COLUMNS_KEY)!)).toEqual(['group', 'daily_usage', 'monthly_usage'])
  })

  it('toggles and reloads period columns independently without clearing selected subscriptions', async () => {
    const wrapper = await renderRows()
    const table = wrapper.getComponent(DataTableStub)
    table.vm.$emit('update:selectedKeys', [9])
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([9])
    await openColumnSettings(wrapper)
    await togglePeriod(wrapper, 'Daily')
    expect(wrapper.find('th[data-column="daily_usage"]').exists()).toBe(false)
    expect(wrapper.find('th[data-column="weekly_usage"]').exists()).toBe(true)
    expect(wrapper.find('th[data-column="monthly_usage"]').exists()).toBe(true)
    expect(table.props('selectedKeys')).toEqual([9])
    expect(JSON.parse(localStorage.getItem(HIDDEN_COLUMNS_KEY)!)).toEqual(['daily_usage'])

    const reloaded = await renderRows()
    expect(reloaded.find('th[data-column="daily_usage"]').exists()).toBe(false)
    expect(reloaded.find('th[data-column="weekly_usage"]').exists()).toBe(true)
    expect(reloaded.find('th[data-column="monthly_usage"]').exists()).toBe(true)
    await openColumnSettings(reloaded)
    await togglePeriod(reloaded, 'Daily')
    expect(JSON.parse(localStorage.getItem(HIDDEN_COLUMNS_KEY)!)).toEqual([])
    expect(reloaded.findAll('th[data-column$="_usage"]')).toHaveLength(3)
  })
})
