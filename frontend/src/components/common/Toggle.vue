<template>
  <button
    type="button"
    @click="toggle"
    class="ag-switch inline-flex flex-shrink-0 items-center gap-2"
    role="switch"
    :aria-checked="modelValue"
  >
    <span
      class="ag-switch-indicator"
      aria-hidden="true"
    >{{ modelValue ? '✓' : '×' }}</span>
    <span class="ag-switch-label">{{ t(modelValue ? 'common.enabled' : 'common.disabled') }}</span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  emit('update:modelValue', !props.modelValue)
}
</script>
