import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardActiveNow from '../UserDashboardActiveNow.vue'
import type { ActiveNowWindow } from '../types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const windows: ActiveNowWindow[] = (['60m', '12h', '24h'] as const).map((id) => ({ id, requests: 4, tokens: 400, rpm: 2, tpm: 200, modelShares: [{ model: 'model-a', requests: 4, share: 1 }], lastCallAt: '2026-10-31T12:59:00Z', complete: true, cappedPages: null }))

describe('UserDashboardActiveNow', () => {
  it('shows all windows, RPM/TPM, model share, and last-call age', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-31T13:00:00Z'))
    const wrapper = mount(UserDashboardActiveNow, { props: { windows, selectedWindow: '60m', loading: false }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.findAll('[role="tab"]').length).toBe(3)
    expect(wrapper.text()).toContain('accountInsights.active.rpm')
    expect(wrapper.text()).toContain('accountInsights.active.tpm')
    expect(wrapper.text()).toContain('model-a')
    expect(wrapper.text()).toContain('1m')
    vi.useRealTimers()
  })

  it('preserves precision for sub-one RPM values', () => {
    const lowRpmWindows = windows.map((window) => ({ ...window, requests: window.id === '60m' ? 4 : 0, rpm: window.id === '60m' ? 4 / 60 : 0, tpm: window.id === '60m' ? 400 / 60 : 0 }))
    const wrapper = mount(UserDashboardActiveNow, { props: { windows: lowRpmWindows, selectedWindow: '60m', loading: false }, global: { stubs: { LoadingSpinner: true } } })
    expect(wrapper.text()).toContain('0.07')
  })

  it('emits window changes and shows an exact cap notice without claiming completeness', async () => {
    const capped = windows.map((window) => ({ ...window, complete: false, cappedPages: 5 }))
    const wrapper = mount(UserDashboardActiveNow, { props: { windows: capped, selectedWindow: '12h', loading: false }, global: { stubs: { LoadingSpinner: true } } })
    await wrapper.findAll('[role="tab"]')[2].trigger('click')
    expect(wrapper.emitted('update:selectedWindow')).toEqual([[ '24h' ]])
    expect(wrapper.text()).toContain('accountInsights.active.cappedDescription')
    expect(wrapper.text()).not.toContain('complete')
  })
})
