import { enableAutoUnmount, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { currencyRate } from '@/utils/currency'
import AppHeader from '../AppHeader.vue'

const state = vi.hoisted(() => ({
  enabled: true,
  user: { username: 'Studio customer', email: 'customer@example.invalid', role: 'user', balance: 0, frozen_balance: 0, avatar_url: '' },
  replay: vi.fn(), logout: vi.fn(), push: vi.fn()
}))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(false) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: state.push }),
  useRoute: () => ({ name: 'Dashboard', params: {}, meta: {} })
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ sidebarCollapsed: false, mobileOpen: false, siteLogo: '', contactInfo: '', docUrl: '', cachedPublicSettings: { subscription_enabled: state.enabled }, toggleSidebar: vi.fn(), toggleMobileSidebar: vi.fn() }),
  useAuthStore: () => ({ user: state.user, isAdmin: false, isSimpleMode: false, logout: state.logout }),
  useOnboardingStore: () => ({ replay: state.replay })
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription', modelPlaza: 'modelPlaza' }, isFeatureFlagEnabled: (flag: string) => flag === 'subscription' && state.enabled }))
vi.mock('@/router/title', () => ({ resolveRouteMetaKeys: () => ({}) }))
vi.mock('@/utils/siteBillingMode', () => ({ resolveSiteBillingMode: () => 'hybrid' }))

enableAutoUnmount(afterEach)
const render = () => mount(AppHeader, { global: {
  stubs: {
    Icon: true, CurrencyMenu: true, LocaleSwitcher: true, AnnouncementBell: true,
    SubscriptionProgressMini: { template: '<div data-testid="subscription-stub">Plan daily remaining ¥0.00</div>' },
    RouterLink: { template: '<a><slot /></a>' }
  }
} })
beforeEach(() => {
  state.enabled = true
  state.user.balance = 0
  currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
})
afterEach(() => { currencyRate.value = null })

describe('subscription-aware AppHeader', () => {
  it('keeps subscription allowance and cash distinct, including real zero cash', () => {
    const wrapper = render()
    expect(wrapper.get('[data-testid="subscription-stub"]').text()).toContain('¥0.00')
    const cash = wrapper.get('[data-testid="header-cash-balance"]')
    expect(cash.text()).toContain('subscriptionProgress.cashBalance')
    expect(cash.text()).toContain('¥0.00')
    expect(cash.classes()).not.toContain('hidden')
  })

  it('does not mount the subscription surface when subscription_enabled is false', () => {
    state.enabled = false
    const wrapper = render()
    expect(wrapper.find('[data-testid="subscription-stub"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="header-cash-balance"]').exists()).toBe(true)
  })

  it('uses two decimal USD fallback when no exchange rate exists', () => {
    currencyRate.value = null
    state.user.balance = 0.04
    expect(render().get('[data-testid="header-cash-balance"]').text()).toContain('$0.04')
  })

  it('keeps compact-screen controls accessible through the user menu', async () => {
    const wrapper = render()
    await wrapper.get('button[aria-label="common.userMenu"]').trigger('click')
    expect(wrapper.find('currency-menu-stub').exists()).toBe(true)
    expect(wrapper.find('locale-switcher-stub').exists()).toBe(true)
    expect(wrapper.text()).toContain('subscriptionProgress.cashBalance')
  })
})
