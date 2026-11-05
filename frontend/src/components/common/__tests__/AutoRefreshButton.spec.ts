import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AutoRefreshButton from '../AutoRefreshButton.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('AutoRefreshButton', () => {
  // Pages pair this with a manual refresh button; a timer glyph keeps the two apart.
  it('shows a timer glyph, not a second refresh glyph', () => {
    const wrapper = mount(AutoRefreshButton, {
      props: { enabled: true, intervalSeconds: 30, countdown: 12, intervals: [15, 30] }
    })
    const html = wrapper.html()
    expect(html).not.toContain('M19 9V5H15M19 5L15 9')
    expect(wrapper.get('svg path').attributes('d')).toBe('M4 4H20V20H4ZM12 7V12H16')
    wrapper.unmount()
  })
})
