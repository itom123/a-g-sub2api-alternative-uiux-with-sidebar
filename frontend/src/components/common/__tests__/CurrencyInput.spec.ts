import { mount } from '@vue/test-utils'
import { afterEach, expect, it } from 'vitest'
import CurrencyInput from '../CurrencyInput.vue'
import { currencyRate } from '@/utils/currency'

afterEach(() => { currencyRate.value = null })

it('shows CNY but emits the original USD units without rounding', async () => {
  currencyRate.value = { cnyPerUsd: 7, source: 'ECB', rateDate: '2026-10-09', updatedAt: '', nextRefreshAt: '' }
  const wrapper = mount(CurrencyInput, { props: { modelValue: 10 }, attrs: { min: 0, step: 'any' } })
  expect(wrapper.get('input').element.value).toBe('70')
  await wrapper.get('input').setValue('165')
  expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe(165 / 7)
  await wrapper.get('input').setValue('')
  expect(wrapper.emitted('update:modelValue')?.[1]?.[0]).toBe('')
})

it('disables financial input when no authoritative rate is available', () => {
  currencyRate.value = null
  const wrapper = mount(CurrencyInput, { props: { modelValue: 10 } })
  expect(wrapper.get('input').attributes('disabled')).toBeDefined()
})
