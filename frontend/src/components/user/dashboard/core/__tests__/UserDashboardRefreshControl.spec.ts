import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserDashboardRefreshControl from '../UserDashboardRefreshControl.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
describe('UserDashboardRefreshControl', () => {
  it('renders all five refresh choices and selected value', () => { const w = mount(UserDashboardRefreshControl, { props: { seconds: 30, countdown: 30 } }); expect(w.findAll('option').map(x => x.attributes('value'))).toEqual(['0', '30', '60', '300', '900']); expect((w.get('select').element as HTMLSelectElement).value).toBe('30') })
  it('emits selected interval and hidden pause state', async () => { const w = mount(UserDashboardRefreshControl, { props: { seconds: 30, countdown: 20, hidden: true } }); await w.get('select').setValue('300'); expect(w.emitted('update:seconds')?.[0]).toEqual([300]); expect(w.text()).toContain('accountInsights.refreshInterval.pausedHidden') })
})
