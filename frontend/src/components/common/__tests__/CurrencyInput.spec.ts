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

it('rounds quota inputs in CNY before converting back to precise USD', async () => {
  currencyRate.value = { cnyPerUsd: 7.123456, source: 'ECB', rateDate: '', updatedAt: '', nextRefreshAt: '' }
  const wrapper = mount(CurrencyInput, { props: { modelValue: 10, whole: true }, attrs: { min: 0, step: '0.01' } })
  expect(wrapper.get('input').element.value).toBe('71')
  expect(wrapper.get('input').attributes('step')).toBe('1')
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  await wrapper.get('input').setValue('72.5')
  expect(wrapper.get('input').element.value).toBe('73')
  expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe(73 / 7.123456)
  await wrapper.get('input').setValue('0')
  expect(wrapper.emitted('update:modelValue')?.[1]?.[0]).toBe(0)
})

it('keeps a positive group quota at one yuan minimum instead of rounding it to unlimited', async () => {
  currencyRate.value = { cnyPerUsd: 7, source: 'test', rateDate: '', updatedAt: '', nextRefreshAt: '' }
  const wrapper = mount(CurrencyInput, { props: { modelValue: 0.01, whole: true }, attrs: { min: 1 } })
  expect(wrapper.get('input').element.value).toBe('1')
  await wrapper.get('input').setValue('0.2')
  expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe(1 / 7)
  await wrapper.get('input').setValue('')
  expect(wrapper.emitted('update:modelValue')?.[1]?.[0]).toBe('')
})
