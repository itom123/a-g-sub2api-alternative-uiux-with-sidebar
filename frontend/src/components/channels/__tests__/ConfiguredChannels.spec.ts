import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ConfiguredChannels from '../ConfiguredChannels.vue'
import { list } from '@/api/admin/channels'

vi.mock('@/api/admin/channels', () => ({ list: vi.fn().mockResolvedValue({ items: [{ id: 1, name: '通用', description: '日常任务', status: 'active', group_ids: [8], model_mapping: { openai: { 'gpt-6-luna': 'gpt-6-luna' } }, restrict_models: false }], total: 1 }) }))
vi.mock('@/api/admin/groups', () => ({ getAllIncludingInactive: vi.fn().mockResolvedValue([{ id: 8, name: 'openai-通用', platform: 'openai', rate_multiplier: .3, status: 'active' }]) }))

afterEach(() => vi.clearAllMocks())
describe('configured channel management summary', () => {
  it('shows actual mappings and explains they do not confer personal access', async () => {
    const wrapper = mount(ConfiguredChannels, { props: { search: '' }, global: { stubs: { RouterLink: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('不代表管理员个人密钥')
    expect(wrapper.text()).toContain('gpt-6-luna')
    expect(wrapper.text()).toContain('0.3×')
    expect(wrapper.text()).not.toContain('openai-通用')
    await wrapper.setProps({ search: 'missing-model' })
    expect(wrapper.findAll('.ag-channel')).toHaveLength(0)
    await wrapper.setProps({ search: 'luna' })
    expect(wrapper.findAll('.ag-channel')).toHaveLength(1)
    wrapper.unmount()
  })
  it('reports failed refresh instead of implying there are no channels', async () => {
    vi.mocked(list).mockRejectedValueOnce(new Error('temporary failure'))
    const wrapper = mount(ConfiguredChannels, { props: { search: '' }, global: { stubs: { RouterLink: true } } })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('配置读取失败')
    wrapper.unmount()
  })
})
