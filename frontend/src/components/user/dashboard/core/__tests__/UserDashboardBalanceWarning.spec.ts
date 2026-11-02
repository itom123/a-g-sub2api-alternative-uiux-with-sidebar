import { mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import UserDashboardBalanceWarning from '../UserDashboardBalanceWarning.vue'
import { currencyRate } from '@/utils/currency'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const push = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ publicSettingsLoaded: true, cachedPublicSettings: { payment_enabled: true }, fetchPublicSettings: vi.fn() }) }))
const profile = { id: 42, balance: 10 } as any
function render(over: Record<string, unknown> = {}) { return mount(UserDashboardBalanceWarning, { props: { profile, visible: true, thresholdCny: 210, dismissed: false, ...over } }) }
beforeEach(() => { localStorage.clear(); push.mockReset(); currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' } })
describe('UserDashboardBalanceWarning', () => {
  it('uses the exact default threshold and supports custom threshold', async () => { const w = render(); expect((w.get('input[type=number]').element as HTMLInputElement).value).toBe('210'); await w.get('input[type=number]').setValue('300'); await w.get('input[type=number]').trigger('change'); expect(localStorage.getItem('account-insights:warning-threshold:42')).toBe('300') })
  it('dismisses per user and emits the dashboard event', async () => { const w = render(); await w.findAll('button')[2].trigger('click'); expect(localStorage.getItem('account-insights:warning-dismissed:42')).toBe('1') })
  it('navigates to purchase and falls back to redeem on failure', async () => { const w = render(); await w.get('button.btn-primary').trigger('click'); await Promise.resolve(); expect(push).toHaveBeenCalledWith('/purchase'); push.mockRejectedValueOnce(new Error('disabled')); await w.get('button.btn-primary').trigger('click'); await Promise.resolve(); expect(push).toHaveBeenLastCalledWith('/redeem') })
  it('does not render while currency rate is unavailable', () => { currencyRate.value = null; expect(render({ visible: false }).find('[data-testid=balance-warning]').exists()).toBe(false) })
})
