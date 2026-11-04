import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'

import SubscriptionBalanceOverflowCard from '../SubscriptionBalanceOverflowCard.vue'

const { updateProfile, showSuccess, showError, authState } = vi.hoisted(() => ({
  updateProfile: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  authState: { user: null as Record<string, unknown> | null }
}))

vi.mock('@/api', () => ({ userAPI: { updateProfile } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
const store = reactive(authState)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => store }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const toggle = (wrapper: ReturnType<typeof mount>) => wrapper.get('[data-testid="balance-overflow-toggle"]')

describe('SubscriptionBalanceOverflowCard', () => {
  beforeEach(() => {
    updateProfile.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
  })

  it('lets the user turn balance overflow off and back on', async () => {
    store.user = { id: 4, balance_overflow_admin_allowed: true, balance_overflow_enabled: true }
    updateProfile.mockImplementation(async (body: { balance_overflow_enabled: boolean }) => ({ ...store.user, ...body }))
    const wrapper = mount(SubscriptionBalanceOverflowCard)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('true')

    await toggle(wrapper).trigger('click')
    await flushPromises()
    expect(updateProfile).toHaveBeenLastCalledWith({ balance_overflow_enabled: false })
    expect(toggle(wrapper).attributes('aria-checked')).toBe('false')

    await toggle(wrapper).trigger('click')
    await flushPromises()
    expect(updateProfile).toHaveBeenLastCalledWith({ balance_overflow_enabled: true })
    expect(toggle(wrapper).attributes('aria-checked')).toBe('true')
  })

  it('shows off and disabled when the admin disallows it, and never calls the API', async () => {
    store.user = { id: 4, balance_overflow_admin_allowed: false, balance_overflow_enabled: true }
    const wrapper = mount(SubscriptionBalanceOverflowCard)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('false')
    expect(toggle(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="balance-overflow-admin-disabled"]').exists()).toBe(true)

    await toggle(wrapper).trigger('click')
    await flushPromises()
    expect(updateProfile).not.toHaveBeenCalled()
  })

  it('keeps the previous state and reports the error when saving fails', async () => {
    store.user = { id: 4, balance_overflow_admin_allowed: true, balance_overflow_enabled: true }
    updateProfile.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mount(SubscriptionBalanceOverflowCard)
    await toggle(wrapper).trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalled()
    expect(toggle(wrapper).attributes('aria-checked')).toBe('true')
  })
})
