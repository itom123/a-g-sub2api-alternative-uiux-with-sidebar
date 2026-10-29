import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'
import ChannelsView from '@/views/admin/ChannelsView.vue'

const { listChannels, getGroups, syncPricingModels, showSuccess, showError } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  getGroups: vi.fn(),
  syncPricingModels: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, syncPricingModels },
    groups: { getAll: getGroups },
    settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }
  }
}))

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        }),
        PricingEntryCard: defineComponent({
          name: 'PricingEntryCard',
          props: ['entry'],
          template: '<div class="pricing-entry">{{ entry.models.join(", ") }}</div>'
        }),
        DataTable: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        Toggle: true
      }
    }
  })
}

async function openDialog(wrapper: ReturnType<typeof mountView>) {
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.channels.createChannel')!.trigger('click')
  await flushPromises()
}

async function enablePlatform(wrapper: ReturnType<typeof mountView>, platform: string) {
  const label = wrapper.findAll('label').find(label => label.text() === `admin.groups.platforms.${platform}`)
  expect(label).toBeDefined()
  await label!.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button.channel-tab').find(button => button.text() === `admin.groups.platforms.${platform}`)!.trigger('click')
}

function visibleSyncButtons(wrapper: ReturnType<typeof mountView>) {
  return wrapper.findAll('button').filter(button => button.isVisible() && [
    'admin.channels.form.syncLatestModels',
    'admin.channels.form.syncingModels'
  ].includes(button.text()))
}

describe('ChannelsView pricing model sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listChannels.mockResolvedValue({ items: [], total: 0 })
    getGroups.mockResolvedValue([])
    syncPricingModels.mockReset()
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it.each([
    ['openai', true],
    ['opencode_go', true],
    ['typesafe', true],
    ['command_code', false],
    ['cline', false]
  ] as const)('offers pricing model sync for %s only when its pricing catalog exists', async (platform, expected) => {
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform)

    expect(visibleSyncButtons(wrapper)).toHaveLength(expected ? 1 : 0)
    expect(syncPricingModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reacts to a new platform gaining a pricing catalog', async () => {
    const platform = { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, platform]
    })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, platform.id)
    expect(visibleSyncButtons(wrapper)).toHaveLength(0)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, { ...platform, litellm_provider: 'acme' }]
    })
    await flushPromises()
    expect(visibleSyncButtons(wrapper)).toHaveLength(1)
    wrapper.unmount()
  })

  it('blocks concurrent syncs and waits for a model selection before changing the draft', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    syncPricingModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await enablePlatform(wrapper, 'opencode_go')

    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeDefined()
    await visibleSyncButtons(wrapper)[0].trigger('click')
    expect(syncPricingModels).toHaveBeenCalledOnce()
    expect(syncPricingModels).toHaveBeenCalledWith('openai')

    resolveSync({ models: ['gpt-latest'] })
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(0)
    await enablePlatform(wrapper, 'openai')
    await wrapper.get('[data-testid="channel-model-picker"] input[value="gpt-latest"]').setValue(true)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.addSelectedModels')!.trigger('click')
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(1)
    expect(wrapper.get('.pricing-entry').text()).toBe('gpt-latest')
    expect(visibleSyncButtons(wrapper)[0].attributes('disabled')).toBeUndefined()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('adds a 150-model catalog with independent default-price rules and matching mappings', async () => {
    const models = Array.from({ length: 150 }, (_, i) => `gpt-model-${String(i).padStart(3, '0')}`)
    syncPricingModels.mockResolvedValue({ models: [...models, models[0]] })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(0)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.selectVisibleModels')!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.addSelectedModels')!.trigger('click')
    const entries = wrapper.findAllComponents({ name: 'PricingEntryCard' })
    expect(entries).toHaveLength(150)
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(150)
    const sources = wrapper.findAll('.channel-mapping-list input[placeholder="admin.channels.form.mappingSource"]').map(input => (input.element as HTMLInputElement).value)
    expect(sources).toEqual(models)
    expect(wrapper.findAll('.channel-mapping-list input[placeholder="admin.channels.form.mappingTarget"]').map(input => (input.element as HTMLInputElement).value)).toEqual(models)
    expect(entries.every(entry => entry.props('entry').input_price === null)).toBe(true)
    expect(syncPricingModels).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('selects only matching catalog results and keeps existing aliases and prices', async () => {
    syncPricingModels.mockResolvedValue({ models: ['gpt-new', 'gpt-kept', 'other-new'] })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    const addButtons = wrapper.findAll('button').filter(button => button.isVisible() && button.text() === '+ common.add')
    await addButtons[1].trigger('click')
    const pricing = wrapper.findComponent({ name: 'PricingEntryCard' })
    pricing.vm.$emit('update', { ...pricing.props('entry'), models: ['gpt-kept'], input_price: 99 })
    await flushPromises()
    const source = wrapper.get('.channel-mapping-list input[placeholder="admin.channels.form.mappingSource"]')
    await source.setValue('friendly-alias')
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="channel-model-picker"] input[type="search"]').setValue('gpt')
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.selectVisibleModels')!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.addSelectedModels')!.trigger('click')
    expect(wrapper.findAll('.pricing-entry').map(e => e.text())).toEqual(['gpt-kept', 'gpt-new'])
    expect(pricing.props('entry').input_price).toBe(99)
    expect(wrapper.findAll('.channel-mapping-list input[placeholder="admin.channels.form.mappingSource"]').map(input => (input.element as HTMLInputElement).value)).toEqual(['friendly-alias', 'gpt-new'])
    wrapper.unmount()
  })

  it('does not add catalog models already covered by a case-insensitive wildcard price', async () => {
    syncPricingModels.mockResolvedValue({ models: ['GPT-LATEST', 'gpt-small'] })
    const wrapper = mountView()
    await openDialog(wrapper)
    await enablePlatform(wrapper, 'openai')
    await wrapper.findAll('button').filter(button => button.isVisible() && button.text() === '+ common.add')[1].trigger('click')
    const pricing = wrapper.findComponent({ name: 'PricingEntryCard' })
    pricing.vm.$emit('update', { ...pricing.props('entry'), models: ['gpt-*'], input_price: 2 })
    await flushPromises()
    await visibleSyncButtons(wrapper)[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="channel-model-picker"]').exists()).toBe(false)
    expect(wrapper.findAll('.pricing-entry')).toHaveLength(1)
    expect(wrapper.find('.channel-mapping-list').exists()).toBe(false)
    wrapper.unmount()
  })
})
