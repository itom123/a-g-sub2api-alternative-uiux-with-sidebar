<script setup lang="ts">
import { cnyNumber } from '@/utils/currency'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  used: number
  limit: number
  label?: string // 文字前缀，如 "D" / "W"；不传时显示 icon
}>()

const { t } = useI18n()

const badgeClass = computed(() => {
  if (props.used >= props.limit) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  }
  if (props.used >= props.limit * 0.8) {
    return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  }
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})

const tooltip = computed(() => {
  if (props.used >= props.limit) {
    return t('admin.accounts.capacity.quota.exceeded')
  }
  return t('admin.accounts.capacity.quota.normal')
})

const fmt = (v: number) => cnyNumber(v)
</script>

<template>
  <span
    :class="[
      'inline-flex items-center gap-1 rounded-md px-1.5 py-px text-[10px] font-medium leading-tight',
      badgeClass
    ]"
    :title="tooltip"
  >
    <span v-if="label" class="font-semibold opacity-70">{{ label }}</span>
    <svg v-else class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="square" stroke-linejoin="miter" fill="none" stroke="currentColor" stroke-width="1.5" d="M2 4H22V20H2ZM2 9H22M6 15H12" />
    </svg>
    <span class="font-mono">¥{{ fmt(used) }}</span>
    <span class="text-gray-400 dark:text-gray-500">/</span>
    <span class="font-mono">¥{{ fmt(limit) }}</span>
  </span>
</template>
