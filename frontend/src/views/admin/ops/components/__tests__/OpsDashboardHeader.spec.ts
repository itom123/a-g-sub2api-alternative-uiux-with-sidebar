import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import type { OpsDashboardOverview } from '@/api/admin/ops'

vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ enabled: true, summary: null }) } }))
vi.mock('@/stores', () => ({ useAdminSettingsStore: () => ({ opsRealtimeMonitoringEnabled: true, setOpsRealtimeMonitoringEnabledLocal: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key })
}))

const overview = { tps: { avg: 242.7 } } as OpsDashboardOverview
function mountHeader(value = overview) {
  return mount(OpsDashboardHeader, {
    props: { overview: value, platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null },
    global: { stubs: { Select: true, HelpTooltip: true, BaseDialog: true, Icon: true } }
  })
}

describe('OpsDashboardHeader output TPS', () => {

  it('shows per-request P50, slower-tail percentiles and sample count separately from traffic TPS', async () => {
    const wrapper = mountHeader({ ...overview, output_tps: { p5: 2.35, p10: 3.7, p50: 55, avg: 277.75, sample_count: 4 } })
    await flushPromises()
    const card = wrapper.get('[data-testid="output-tps-card"]')
    expect(card.get('[data-testid="output-tps-p50"]').text()).toBe('55.0')
    expect(card.get('[data-testid="output-tps-p5"]').text()).toBe('2.4')
    expect(card.get('[data-testid="output-tps-p10"]').text()).toBe('3.7')
    expect(card.get('[data-testid="output-tps-avg"]').text()).toBe('277.8')
    expect(card.get('[data-testid="output-tps-samples"]').text()).toContain('4')
    expect(card.text()).toContain('tok/s (P50)')
    expect(card.text()).not.toContain('242.7')
    await wrapper.setProps({ fullscreen: true, overview: { ...overview, output_tps: { p5: 0.01, p10: 0.02, p50: 0.04, avg: 0.05, sample_count: 1 } } })
    expect(card.get('[data-testid="output-tps-p50"]').text()).toBe('0.0')
    expect(card.get('[data-testid="output-tps-samples"]').text()).toContain('1')
    wrapper.unmount()
  })

  it.each([undefined, null, { p5: null, p10: null, p50: null, avg: null, sample_count: 0 }])('shows missing output TPS as unavailable, not zero: %s', async (output_tps) => {
    const wrapper = mountHeader({ ...overview, output_tps })
    await flushPromises()
    for (const metric of ['p5', 'p10', 'p50', 'avg']) {
      expect(wrapper.get(`[data-testid="output-tps-${metric}"]`).text()).toBe('—')
    }
    expect(wrapper.get('[data-testid="output-tps-samples"]').text()).toContain(output_tps == null ? '—' : '0')
    wrapper.unmount()
  })

})

describe('OpsDashboardHeader latency diagnosis', () => {
  // Not idle: diagnosis only lists metric findings when there is traffic.
  const busy = { ...overview, qps: { current: 0.2, peak: 0.3, avg: 0.1 }, error_rate: 0, upstream_error_rate: 0, sla: 1 } as OpsDashboardOverview

  it('flags TTFT on P90 above 5s, not on a long-prompt P99 tail', async () => {
    const calm = mountHeader({ ...busy, ttft: { p50_ms: 1815, p90_ms: 4169, p99_ms: 11977 } })
    await flushPromises()
    expect(calm.text()).not.toContain('admin.ops.diagnosis.ttftHigh')
    calm.unmount()

    const slow = mountHeader({ ...busy, ttft: { p50_ms: 3088, p90_ms: 6678, p99_ms: 11977 } })
    await flushPromises()
    expect(slow.text()).toContain('admin.ops.diagnosis.ttftHigh:{"ttft":"6678"}')
    slow.unmount()
  })

  it('shows gateway overhead on the TTFT card and warns only above 300ms', async () => {
    const fast = mountHeader({
      ...busy,
      ttft: { p90_ms: 4000 },
      gateway_overhead: {
        window_minutes: 60, sample_count: 353, p50_ms: 55, p90_ms: 68, p99_ms: 120, max_ms: 410,
        processing_p90_ms: 41, routing_p90_ms: 33, upload_p50_ms: 120, upload_p90_ms: 640, upload_max_ms: 2900
      }
    })
    await flushPromises()
    expect(fast.get('[data-testid="gateway-overhead-p90"]').text()).toBe('68')
    expect(fast.get('[data-testid="gateway-overhead-scope"]').text()).toContain('{"minutes":60}')
    // A slow client upload is shown on its own line and does not raise the gateway warning.
    expect(fast.get('[data-testid="gateway-upload-p90"]').text()).toBe('640')
    expect(fast.get('[data-testid="gateway-upload-p90"]').classes()).not.toContain('text-yellow-600')
    expect(fast.get('[data-testid="gateway-overhead-p90"]').classes()).not.toContain('text-yellow-600')
    expect(fast.text()).not.toContain('admin.ops.diagnosis.gatewayOverheadHigh')
    fast.unmount()

    const slow = mountHeader({
      ...busy,
      ttft: { p90_ms: 4000 },
      gateway_overhead: { window_minutes: 15, sample_count: 40, p90_ms: 420, processing_p90_ms: 380, routing_p90_ms: 40 }
    })
    await flushPromises()
    expect(slow.get('[data-testid="gateway-overhead-p90"]').classes()).toContain('text-yellow-600')
    expect(slow.text()).toContain('admin.ops.diagnosis.gatewayOverheadHigh:{"ms":"420"}')
    expect(slow.text()).toContain('admin.ops.diagnosis.gatewayOverheadHighAction:{"processing":"380","routing":"40"}')
    expect(slow.find('[data-testid="gateway-upload"]').exists()).toBe(false)
    slow.unmount()
  })

  it('hides gateway overhead when the overview has none (filtered view)', async () => {
    const wrapper = mountHeader({ ...busy, ttft: { p90_ms: 4000 } })
    await flushPromises()
    expect(wrapper.find('[data-testid="gateway-overhead"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('OpsDashboardHeader health frame', () => {
  const busy = { ...overview, qps: { current: 0.2, peak: 0.3, avg: 0.1 }, health_score: 75 } as OpsDashboardOverview

  it('draws the health score as a right-angle frame, not a round arc', async () => {
    const wrapper = mountHeader(busy)
    await flushPromises()
    const ring = wrapper.get('.ag-ops-ring svg')
    expect(ring.findAll('circle')).toHaveLength(0)
    const [track, progress] = ring.findAll('path')
    // 100px frame, 8px stroke: centred path from 4 to 96, starting at 12 o'clock.
    expect(track.attributes('d')).toBe('M50 4H96V96H4V4Z')
    expect(progress.attributes('d')).toBe(track.attributes('d'))
    expect(progress.attributes('stroke-linejoin')).toBe('miter')
    expect(progress.attributes('stroke-linecap')).toBe('butt')
    expect(Number(progress.attributes('stroke-dasharray'))).toBe(368)
    expect(Number(progress.attributes('stroke-dashoffset'))).toBeCloseTo(92)
    wrapper.unmount()
  })
})
