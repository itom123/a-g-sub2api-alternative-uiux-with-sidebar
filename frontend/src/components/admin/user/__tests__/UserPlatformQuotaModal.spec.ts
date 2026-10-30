import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { AdminUser } from '@/types'
import { currencyRate } from '@/utils/currency'
import UserPlatformQuotaModal from '../UserPlatformQuotaModal.vue'

const mocks = vi.hoisted(() => ({
  getPlatformQuotas: vi.fn(),
  updatePlatformQuotas: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { users: mocks } }))
vi.mock('@/api/admin/users', () => ({ platformQuotaPlatforms: () => ['openai'] }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  currencyRate.value = {
    cnyPerUsd: 6.692129,
    source: 'test',
    rateDate: '2026-10-09',
    updatedAt: '',
    nextRefreshAt: '',
  }
  mocks.getPlatformQuotas.mockResolvedValue({
    platform_quotas: [{
      platform: 'openai',
      daily_limit_usd: 3,
      weekly_limit_usd: 10,
      monthly_limit_usd: 10,
      daily_usage_usd: 3.042337716,
      weekly_usage_usd: 5.5,
      monthly_usage_usd: 7.25,
    }],
  })
  mocks.updatePlatformQuotas.mockResolvedValue({})
})

async function openDialog() {
  const wrapper = mount(UserPlatformQuotaModal, {
    props: { show: false, user: { id: 12, email: 'user@example.com' } as AdminUser },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('UserPlatformQuotaModal currency display', () => {
  it('converts daily, weekly and rolling monthly usage to labelled CNY', async () => {
    const wrapper = await openDialog()
    expect(wrapper.get('tbody tr td:last-child').text()).toBe('\u00a520 / \u00a537 / \u00a549')
    expect(wrapper.findAll('input').map(input => input.element.value)).toEqual([
      '20', '67', '67',
    ])
  })

  it('updates displayed usage when the shared exchange rate changes', async () => {
    const wrapper = await openDialog()
    currencyRate.value = { ...currencyRate.value!, cnyPerUsd: 7 }
    await nextTick()
    expect(wrapper.get('tbody tr td:last-child').text()).toBe('\u00a521 / \u00a539 / \u00a551')
  })

  it('does not display USD as CNY when the exchange rate is unavailable', async () => {
    currencyRate.value = null
    const wrapper = await openDialog()
    expect(wrapper.get('tbody tr td:last-child').text()).toBe('- / - / -')
  })

  it('saves the whole-yuan limits shown in the form using precise USD units', async () => {
    mocks.getPlatformQuotas.mockResolvedValue({
      platform_quotas: [{
        platform: 'openai',
        daily_limit_usd: 3,
        weekly_limit_usd: 10,
        monthly_limit_usd: 10,
        daily_usage_usd: 0,
        weekly_usage_usd: 0,
        monthly_usage_usd: 0,
      }],
    })
    const wrapper = await openDialog()
    expect(wrapper.get('tbody tr td:last-child').text()).toBe('\u00a50 / \u00a50 / \u00a50')
    await wrapper.findAll('button').find(button => button.text() === 'admin.users.platformQuota.save')!.trigger('click')
    await flushPromises()
    expect(mocks.updatePlatformQuotas).toHaveBeenCalledWith(12, [{
      platform: 'openai', daily_limit_usd: 20 / 6.692129, weekly_limit_usd: 67 / 6.692129, monthly_limit_usd: 67 / 6.692129,
    }])
  })
})
