import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  getAllGroups,
  listGroups,
  duplicateGroup,
  updateGroup,
  getModelAllowlistCandidates,
  getUsageSummary,
  getSubscriptionUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  getAllGroups: vi.fn(),
  listGroups: vi.fn(),
  duplicateGroup: vi.fn(),
  updateGroup: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getSubscriptionUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      duplicate: duplicateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: getAllGroups,
      create: vi.fn(),
      update: updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    subscriptions: {
      getUsageSummary: getSubscriptionUsageSummary
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const sourceGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  model_allowlist: undefined,
  sort_order: 10
}

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>'
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}


const planA: AdminGroup = { ...sourceGroup, id: 17, name: '订阅-A', subscription_type: 'subscription' }
const planB: AdminGroup = { ...sourceGroup, id: 18, name: '订阅-B', subscription_type: 'subscription' }

async function openEdit(group: AdminGroup) {
  listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
  updateGroup.mockResolvedValue(group)
  const wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find((button) => button.text() === 'common.edit')!.trigger('click')
  await flushPromises()
  return wrapper
}

function sourceSelect(wrapper: ReturnType<typeof mountView>) {
  return wrapper.get('#edit-group-form').findAll('select').find((select) =>
    select.text().includes('admin.groups.subscription.sourceGroupsPlaceholder'))
}

describe('GroupsView subscription sharing', () => {
  beforeEach(() => {
    authState.isSimpleMode = false
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [getAllGroups, listGroups, updateGroup, getModelAllowlistCandidates, getUsageSummary, getSubscriptionUsageSummary, getCapacitySummary, getLiveCapability, showSuccess, showError]) {
      fn.mockReset()
    }
    // Only the full group list (not the current page) feeds the source picker.
    getAllGroups.mockResolvedValue([sourceGroup, planA, planB])
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getSubscriptionUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('edits the ordered subscription sources of a standard group', async () => {
    const wrapper = await openEdit({ ...sourceGroup, subscription_source_group_ids: [17] })
    expect(getAllGroups).toHaveBeenCalled()
    const select = sourceSelect(wrapper)
    expect(select).toBeTruthy()
    expect(wrapper.get('#edit-group-form').text()).toContain('订阅-A')

    await select!.setValue('18')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      subscription_source_any: false,
      subscription_source_group_ids: [17, 18],
    }))
    wrapper.unmount()
  })

  it('hides the source list and submits any-source when the switch is on', async () => {
    const wrapper = await openEdit({ ...sourceGroup, subscription_source_group_ids: [17] })
    expect(sourceSelect(wrapper)).toBeTruthy()

    await wrapper.get('[data-testid="group-source-any-edit"]').trigger('click')
    await flushPromises()
    expect(sourceSelect(wrapper)).toBeUndefined()

    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      subscription_source_any: true,
      subscription_source_group_ids: [],
    }))
    wrapper.unmount()
  })

  it('shows no sharing controls on subscription groups and never submits group overflow', async () => {
    const wrapper = await openEdit({ ...planA })
    expect(sourceSelect(wrapper)).toBeUndefined()
    expect(wrapper.find('[data-testid="group-source-any-edit"]').exists()).toBe(false)

    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    const payload = updateGroup.mock.calls[0][1]
    expect(payload).toMatchObject({ subscription_source_any: false, subscription_source_group_ids: [] })
    expect(payload).not.toHaveProperty('subscription_overflow_to_balance')
    wrapper.unmount()
  })
})
