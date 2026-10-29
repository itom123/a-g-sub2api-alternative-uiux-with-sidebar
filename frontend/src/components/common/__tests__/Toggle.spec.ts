import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import Toggle from '../Toggle.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key === 'common.enabled' ? 'Enabled' : 'Disabled' }) }))

describe('Toggle', () => {
  it('names both states and emits a reversible change', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false } })
    expect(wrapper.get('[role=switch]').attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).toContain('Disabled')
    await wrapper.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.text()).toContain('Enabled')
    expect(wrapper.get('[role=switch]').attributes('aria-checked')).toBe('true')
    await wrapper.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true], [false]])
  })
})
