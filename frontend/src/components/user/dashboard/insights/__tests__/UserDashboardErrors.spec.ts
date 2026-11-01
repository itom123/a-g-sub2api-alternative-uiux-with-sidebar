import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardErrors from '../UserDashboardErrors.vue'
import type { DashboardErrorRow } from '../types'
import { vi } from 'vitest'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const item: DashboardErrorRow = { id: 4, created_at: '2026-10-31T12:00:00Z', model: 'model-a', inbound_endpoint: '/v1/messages', status_code: 429, category: 'rate_limit', platform: 'anthropic', message: 'safe message', key_name: 'my-key' }

describe('UserDashboardErrors', () => {
  it('renders safe error fields and emits detail selection', async () => {
    const wrapper = mount(UserDashboardErrors, { props: { items: [item], loading: false, unavailable: false } })
    await wrapper.get('tbody tr').trigger('click')
    expect(wrapper.emitted('select-error')).toEqual([[4]])
    expect(wrapper.text()).toContain('safe message')
    expect(wrapper.text()).not.toMatch(/error_body|secret|user_id/i)
  })

  it('distinguishes loading, unavailable, and no-error states', async () => {
    const wrapper = mount(UserDashboardErrors, { props: { items: [], loading: true, unavailable: false }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    await wrapper.setProps({ loading: false, unavailable: true })
    expect(wrapper.text()).toContain('accountInsights.errors.unavailable')
    await wrapper.setProps({ unavailable: false })
    expect(wrapper.text()).toContain('accountInsights.errors.noErrors')
  })

  it('renders responsive stacked cards and a fitting desktop table without horizontal clipping', async () => {
    const wrapper = mount(UserDashboardErrors, { props: { items: [item], loading: false, unavailable: false } })
    expect(wrapper.find('.md\\:hidden').exists()).toBe(true)
    expect(wrapper.find('.md\\:hidden article').text()).toContain('/v1/messages')
    expect(wrapper.find('.md\\:block table').classes()).toContain('table-fixed')
    expect(wrapper.find('.md\\:block table').classes()).not.toContain('min-w-[680px]')
    expect(wrapper.find('colgroup col:nth-child(1)').classes()).toContain('min-w-[8rem]')
    expect(wrapper.find('colgroup col:nth-child(4)').classes()).toContain('min-w-[3.5rem]')
    expect(wrapper.find('colgroup col:nth-child(5)').classes()).toContain('min-w-[7rem]')
    expect(wrapper.find('colgroup col:nth-child(6)').classes()).toContain('min-w-[9rem]')
    expect(wrapper.find('thead th:nth-child(4)').classes()).toContain('pr-4')
    expect(wrapper.find('thead th:nth-child(5)').classes()).toContain('pl-4')
    expect(wrapper.find('tbody td:nth-child(1)').classes()).toContain('whitespace-nowrap')
    expect(wrapper.find('tbody td:nth-child(4)').classes()).toContain('whitespace-nowrap')
    expect(wrapper.find('tbody td:nth-child(5)').classes()).toContain('whitespace-nowrap')
    expect(wrapper.find('tbody td:nth-child(6)').classes()).toContain('whitespace-nowrap')
    expect(wrapper.find('tbody td:nth-child(7)').classes()).toContain('break-words')
    expect(wrapper.find('tbody td:nth-child(7)').classes()).toContain('[overflow-wrap:break-word]')
    await wrapper.find('.md\\:hidden article').trigger('click')
    expect(wrapper.emitted('select-error')).toEqual([[4]])
  })
})
