<template>
  <input
    v-bind="$attrs"
    type="number"
    :value="displayValue"
    :disabled="!currencyRate || ($attrs.disabled !== undefined && $attrs.disabled !== false)"
    @input="onInput"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { currencyRate, fromCny, toCny } from '@/utils/currency'

defineOptions({ inheritAttrs: false })
const props = defineProps<{ modelValue?: number | string | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: any] }>()
const displayValue = computed(() => {
  if (props.modelValue == null || props.modelValue === '' || !currencyRate.value) return ''
  return Number(toCny(props.modelValue).toPrecision(12))
})
function onInput(event: Event) {
  const input = event.target as HTMLInputElement
  emit('update:modelValue', input.value === '' ? '' : fromCny(input.valueAsNumber))
}
</script>
