import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserDashboardDatePresets from '../UserDashboardDatePresets.vue'
import type { DashboardRange } from '../../insights/types'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
const range = (over: Partial<DashboardRange> = {}): DashboardRange => ({ startDate: '2026-11-01', endDate: '2026-11-01', timezone: 'Asia/Shanghai', preset: 'today', ...over })
describe('UserDashboardDatePresets', () => {
  it('exposes all six presets plus custom', () => { const w = mount(UserDashboardDatePresets, { props: { range: range() } }); expect(w.findAll('option').map(x => x.attributes('value'))).toEqual(['today', '7d', 'thisWeek', 'lastWeek', 'thisMonth', '30d', 'custom']) })
  it('emits preset and custom date changes with timezone', async () => { const w = mount(UserDashboardDatePresets, { props: { range: range() } }); await w.get('select').setValue('thisWeek'); expect(w.emitted('update:range')?.[0][0]).toMatchObject({ preset: 'thisWeek', timezone: 'Asia/Shanghai' }); await w.setProps({ range: range({ preset: 'custom', startDate: '2026-11-01', endDate: '2026-11-01' }) }); const dates = w.findAll('input[type=date]'); await dates[0].setValue('2026-10-01'); await dates[1].setValue('2026-10-31'); expect(w.find('[role=alert]').exists()).toBe(false) })
  it.each([{ startDate: '2026-11-02', endDate: '2026-11-01' }, { startDate: '2026-10-01', endDate: '2026-11-01' }])('shows invalid custom range for %o', (dates) => { const w = mount(UserDashboardDatePresets, { props: { range: range({ preset: 'custom', ...dates }) } }); expect(w.get('[role=alert]').text()).toContain('accountInsights.range.invalid') })
})
