<template>
  <div class="flex w-full flex-wrap items-end gap-2 sm:w-auto" data-testid="date-presets">
    <label class="sr-only" for="account-range-preset">{{ t('accountInsights.range.label') }}</label>
    <select id="account-range-preset" class="input min-w-32" :value="range.preset" @change="selectPreset(($event.target as HTMLSelectElement).value as DashboardRange['preset'])">
      <option value="today">{{ t('accountInsights.range.today') }}</option>
      <option value="7d">{{ t('accountInsights.range.sevenDays') }}</option>
      <option value="thisWeek">{{ t('accountInsights.range.thisWeek') }}</option>
      <option value="lastWeek">{{ t('accountInsights.range.lastWeek') }}</option>
      <option value="thisMonth">{{ t('accountInsights.range.thisMonth') }}</option>
      <option value="30d">{{ t('accountInsights.range.thirtyDays') }}</option>
      <option value="custom">{{ t('accountInsights.range.custom') }}</option>
    </select>
    <template v-if="range.preset === 'custom'">
      <label class="text-xs text-gray-500">{{ t('accountInsights.range.start') }}<input class="input ml-1" type="date" :value="range.startDate" @change="updateDate('startDate', ($event.target as HTMLInputElement).value)" /></label>
      <label class="text-xs text-gray-500">{{ t('accountInsights.range.end') }}<input class="input ml-1" type="date" :value="range.endDate" @change="updateDate('endDate', ($event.target as HTMLInputElement).value)" /></label>
    </template>
    <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.range.localTimezone', { timezone: range.timezone }) }}</span>
    <p v-if="invalid" class="basis-full text-xs text-amber-700 dark:text-amber-300" role="alert">{{ t('accountInsights.range.invalid') }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DashboardRange } from '../insights/types'
const props = defineProps<{ range: DashboardRange }>()
const emit = defineEmits<{ 'update:range': [range: DashboardRange] }>()
const { t } = useI18n()
const invalid = computed(() => {
  if (props.range.preset !== 'custom') return false
  const start = Date.parse(`${props.range.startDate}T00:00:00`)
  const end = Date.parse(`${props.range.endDate}T00:00:00`)
  return !Number.isFinite(start) || !Number.isFinite(end) || end < start || Math.floor((end - start) / 86400000) + 1 > 30
})
function selectPreset(preset: DashboardRange['preset']) { emit('update:range', { ...props.range, preset }) }
function updateDate(field: 'startDate' | 'endDate', value: string) { emit('update:range', { ...props.range, preset: 'custom', [field]: value }) }
</script>
