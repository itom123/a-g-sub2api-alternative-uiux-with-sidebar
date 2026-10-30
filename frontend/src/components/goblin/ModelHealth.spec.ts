import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import ModelHealth from './ModelHealth.vue'
import StatusFreshness from './StatusFreshness.vue'
const mock = vi.hoisted(() => ({ state: {} as any }))
vi.mock('@/stores/modelStatus', () => ({ useModelStatusStore: () => mock.state }))
beforeEach(() => {
  mock.state = reactive({ row: null, statusError: false, monitorEnabled: true, hasObservation: false, stale: false, interval: 60, filter: { groupIds: [] }, snapshot: null, findModel: () => mock.state.row })
})
describe('live health presentation', () => {
  it('keeps stale or failed observations unknown and links to the exact group and model', async () => {
    mock.state.row = { health: { overall: 'healthy', minimum_sample: 50 }, metrics: { ttft: { p50_ms: 100 } } }
    mock.state.snapshot = { coverage: { data_through: '0001-01-01T00:00:00Z' } }
    const wrapper = mount(ModelHealth, {
      props: { platform: 'openai', model: 'gpt-6.1-terra', groupId: 9 },
      global: { stubs: { RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' } } }
    })
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to')).toEqual({ path: '/monitor', query: { group: '9', platform: 'openai', model: 'openai\u0000gpt-6.1-terra' } })
    expect(wrapper.get('.ag-status').attributes('title')).not.toContain('观测截至')
    mock.state.stale = true
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.ag-status').text()).toBe('待观测')
    expect(wrapper.text()).toContain('观测数据更新延迟')
    mock.state.statusError = true
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.ag-status').text()).toBe('待观测')
    expect(wrapper.text()).toContain('状态获取失败')
  })
  it('keeps absent models observable and updates every authoritative health enum', async () => {
    const wrapper = mount(ModelHealth, { props: { platform: 'openai', model: 'luna' } })
    expect(wrapper.text()).toContain('待观测'); expect(wrapper.text()).toContain('暂无此模型的近期观测记录')
    for (const [state, label] of Object.entries({ unknown: '待观测', healthy: '正常', warning: '性能下降', critical: '异常' })) {
      mock.state.row = { health: { overall: state, minimum_sample: 50 }, metrics: { request_count: 2, ttft: { p50_ms: null, sample_count: 0 } } }
      await wrapper.vm.$nextTick(); expect(wrapper.get('.ag-status').text()).toBe(label)
      expect(wrapper.text()).toContain('首字延迟暂无样本')
    }
  })
  it('distinguishes missing data, failed fetch and a retained stale/backfilling snapshot', async () => {
    const wrapper = mount(StatusFreshness)
    expect(wrapper.text()).toContain('暂无可观测数据')
    mock.state.statusError = true; await wrapper.vm.$nextTick(); expect(wrapper.text()).toContain('获取失败')
    mock.state.snapshot = { coverage: { data_through: '2026-10-11T10:00:00+01:00', computed_at: '2026-10-11T09:02:00Z', coverage_complete: false, bootstrap: { active: true, progress_percent: 20 } }, health: { minimum_sample: 50 } }
    mock.state.hasObservation = true
    mock.state.stale = true; await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('观测截至'); expect(wrapper.text()).toContain('数据更新延迟'); expect(wrapper.text()).toContain('数据补齐中'); expect(wrapper.text()).toContain('20%')
  })
  it('does not display zero timestamps or zero sample thresholds for an empty scoped response', () => {
    mock.state.snapshot = { coverage: { data_through: '0001-01-01T00:00:00Z', computed_at: '0001-01-01T00:00:00Z', coverage_complete: false }, health: { minimum_sample: 0 } }
    const wrapper = mount(StatusFreshness)
    expect(wrapper.text()).toContain('暂无可观测数据')
    expect(wrapper.text()).not.toContain('观测截至')
    expect(wrapper.text()).not.toContain('最低样本 0')
  })
})
