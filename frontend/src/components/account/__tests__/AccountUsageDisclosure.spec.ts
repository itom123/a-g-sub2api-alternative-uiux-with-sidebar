import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, onMounted, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import AccountUsageDisclosure from '../AccountUsageDisclosure.vue'
import UsageProgressBar from '../UsageProgressBar.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') }),
}))

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function mountDisclosure(slot = '<input value="usage">') {
  return mount(AccountUsageDisclosure, {
    attachTo: document.body,
    props: { accountName: 'Review account' },
    slots: { default: slot },
  })
}

describe('AccountUsageDisclosure', () => {
  it('stays compact on hover and keyboard focus; click opens and closes details', async () => {
    const wrapper = mountDisclosure()
    const trigger = wrapper.get('.ag-usage-details-trigger')
    await trigger.trigger('mouseenter')
    trigger.element.focus()
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.querySelector('.ag-usage-popover')).not.toBeNull()
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('keeps one mounted usage component and live summaries in the table while details are open', async () => {
    const mounted = vi.fn()
    const Usage = defineComponent({
      components: { UsageProgressBar },
      setup() { onMounted(mounted); return { value: ref('retained draft'), used: ref(25) } },
      template: '<div><input v-model="value"><button @click="used = 75">Update sample</button><UsageProgressBar detailed label="5h" :utilization="used" color="indigo"/><UsageProgressBar detailed label="7d" :utilization="40" color="emerald"/></div>',
    })
    const wrapper = mount(AccountUsageDisclosure, { attachTo: document.body, props: { accountName: 'Review' }, slots: { default: Usage } })
    const input = wrapper.get('input').element
    await wrapper.get('input').setValue('updated draft')
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    await flushPromises()
    expect(document.querySelector('.ag-usage-popover input')).toBe(input)
    const summary = wrapper.get('.ag-usage-retained-summary')
    expect(summary.findAll('[role="progressbar"]').map(e => e.attributes('aria-valuenow'))).toEqual(['25', '40'])
    document.querySelector<HTMLButtonElement>('.ag-usage-popover button:not([aria-label])')!.click()
    await flushPromises()
    expect(summary.findAll('[role="progressbar"]')[0].attributes('aria-valuenow')).toBe('75')
    expect(wrapper.find('[aria-hidden="true"][style]').exists()).toBe(false)
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    expect(wrapper.get('input').element).toBe(input)
    expect(input.value).toBe('updated draft')
    expect(mounted).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('lets Escape close details and returns focus to the disclosure', async () => {
    const wrapper = mountDisclosure()
    const trigger = wrapper.get('.ag-usage-details-trigger')
    await trigger.trigger('click')
    await flushPromises()
    document.querySelector<HTMLInputElement>('.ag-usage-popover input')!.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    expect(trigger.attributes('aria-controls')).toBe(wrapper.get('.ag-usage-preview-content').attributes('id'))
    wrapper.unmount()
  })

  it('closes on outside click and cleans up its listeners on unmount', async () => {
    const wrapper = mountDisclosure()
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    await flushPromises()
    document.body.click()
    await flushPromises()
    expect(document.querySelector('.ag-usage-popover')).toBeNull()
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    await flushPromises()
    wrapper.unmount()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(document.querySelector('.ag-usage-popover')).toBeNull()
  })

  it('keeps the details open during tooltip or confirmation interactions', async () => {
    const wrapper = mountDisclosure()
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    await flushPromises()
    for (const className of ['ag-help-tooltip', 'modal-content']) {
      const related = document.createElement('div')
      related.className = className
      document.body.append(related)
      related.click()
      await flushPromises()
      expect(document.querySelector('.ag-usage-popover')).not.toBeNull()
    }
    wrapper.unmount()
  })

  it('gives each account its own disclosure region and unique ID', async () => {
    const wrapper = mount(defineComponent({
      components: { AccountUsageDisclosure },
      template: '<div><AccountUsageDisclosure account-name="One">5h · 7d</AccountUsageDisclosure><AccountUsageDisclosure account-name="Two">5h · 7d</AccountUsageDisclosure></div>',
    }), { attachTo: document.body })
    const triggers = wrapper.findAll('.ag-usage-details-trigger')
    await triggers[0].trigger('click')
    expect(triggers.map(e => e.attributes('aria-expanded'))).toEqual(['true', 'false'])
    expect(triggers[0].attributes('aria-controls')).not.toBe(triggers[1].attributes('aria-controls'))
    expect(document.querySelector('[role="dialog"]')?.getAttribute('aria-label')).toContain('One')
    wrapper.unmount()
  })

  it.each([1440, 375])('positions details beside or above the retained summaries at viewport width %s', async (width) => {
    vi.stubGlobal('innerWidth', width)
    vi.stubGlobal('innerHeight', 1000)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const anchor = this.classList.contains('ag-usage-preview')
      const left = anchor ? (width === 1440 ? 1000 : 16) : 0
      const top = anchor ? 700 : 0
      const w = anchor ? (width === 1440 ? 220 : 343) : (width === 1440 ? 448 : 351)
      const height = anchor ? 132 : 700
      return { left, right: left + w, width: w, top, bottom: top + height, height, x: left, y: top, toJSON() {} }
    })
    const wrapper = mountDisclosure()
    await wrapper.get('.ag-usage-details-trigger').trigger('click')
    await flushPromises()
    const panel = document.querySelector<HTMLElement>('.ag-usage-popover')!
    if (width === 1440) {
      expect(panel.style.left).toBe('540px')
      expect(panel.style.top).toBe('288px')
      expect(panel.style.maxHeight).toBe('976px')
    } else {
      expect(panel.style.left).toBe('12px')
      expect(panel.style.bottom).toBe('312px')
      expect(panel.style.maxHeight).toBe('676px')
    }
    panel.dispatchEvent(new Event('scroll', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('.ag-usage-popover')).not.toBeNull()
    wrapper.unmount()
  })
})
