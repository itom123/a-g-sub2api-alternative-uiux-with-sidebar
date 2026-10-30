<template>
  <input
    v-bind="$attrs"
    type="number"
    :step="whole ? 1 : ($attrs.step as string | number | undefined)"
    :value="displayValue"
    :disabled="!currencyRate || ($attrs.disabled !== undefined && $attrs.disabled !== false)"
    @input="onInput"
  />
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'
import { currencyRate, fromCny, toCny } from '@/utils/currency'

defineOptions({ inheritAttrs: false })
const props = defineProps<{ modelValue?: number | string | null; whole?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: any] }>()
const attrs = useAttrs()
const wholeValue = (value: number) => Math.max(Number(attrs.min ?? 0), Math.round(value))
const displayValue = computed(() => {
  if (props.modelValue == null || props.modelValue === '' || !currencyRate.value) return ''
  const value = Number(toCny(props.modelValue).toPrecision(12))
  return props.whole ? wholeValue(value) : value
})
function onInput(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.value === '') {
    emit('update:modelValue', '')
    return
  }
  const value = props.whole ? wholeValue(input.valueAsNumber) : input.valueAsNumber
  if (props.whole) input.value = String(value)
  emit('update:modelValue', fromCny(value))
}
</script>
