import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { useModalScrollLock } from '../useModalScrollLock'
import BaseDialog from '@/components/common/BaseDialog.vue'

enableAutoUnmount(afterEach)

describe('shared modal scroll lock', () => {
  it('retains the wallet dialog lock when an announcement owner closes or unmounts', async () => {
    const visible = ref(true)
    const announcement = mount(defineComponent({ setup() { useModalScrollLock(visible); return () => null } }))
    const wallet = mount(BaseDialog, { props: { show: true, title: 'Wallet' } })
    visible.value = false
    expect(document.body.classList.contains('modal-open')).toBe(true)
    announcement.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    await wallet.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.body.style.overflow).toBe('')
  })
})
