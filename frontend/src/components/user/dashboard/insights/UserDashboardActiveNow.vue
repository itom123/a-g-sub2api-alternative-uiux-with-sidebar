<template>
  <section class="card min-w-0 p-4 xl:col-span-2" data-testid="account-active-now">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2"><div><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.active.title') }}</h3><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.active.subtitle') }}</p></div><div class="flex gap-1" role="tablist" :aria-label="t('accountInsights.active.window')"><button v-for="window in windows" :key="window.id" type="button" role="tab" :aria-selected="selectedWindow === window.id" class="rounded px-2 py-1 text-xs" :class="selectedWindow === window.id ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-gray-300'" @click="emit('update:selectedWindow', window.id)">{{ label(window.id) }}</button></div></div>
    <LoadingSpinner v-if="loading" class="mx-auto" />
    <template v-else-if="current">
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4"><Metric :label="t('accountInsights.active.rpm')" :value="format(current.rpm)" /><Metric :label="t('accountInsights.active.tpm')" :value="format(current.tpm)" /><Metric :label="t('accountInsights.keys.calls')" :value="format(current.requests)" /><Metric :label="t('accountInsights.active.lastCall')" :value="current.lastCallAt ? relative(current.lastCallAt) : t('accountInsights.active.noCall')" /></div>
      <div class="mt-4"><h4 class="mb-2 text-xs font-semibold text-gray-600 dark:text-gray-300">{{ t('accountInsights.active.modelShare') }}</h4><div v-if="current.modelShares.length" class="space-y-2"><div v-for="model in current.modelShares" :key="model.model" class="flex items-center gap-2 text-xs"><span class="w-32 truncate text-gray-700 dark:text-gray-300">{{ model.model }}</span><div class="h-2 flex-1 rounded bg-gray-100 dark:bg-dark-700"><div class="h-2 rounded bg-gray-900 dark:bg-white" :style="{ width: `${model.share * 100}%` }" /></div><span class="w-12 text-right tabular-nums text-gray-500">{{ Math.round(model.share * 100) }}%</span></div></div><p v-else class="text-sm text-gray-500">{{ t('accountInsights.active.noData') }}</p></div>
      <p v-if="!current.complete" class="mt-3 text-xs text-amber-700 dark:text-amber-300">{{ t('accountInsights.active.cappedDescription') }}</p>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { ActiveNowWindow } from './types'
const props = defineProps<{ windows: ActiveNowWindow[]; selectedWindow: ActiveNowWindow['id']; loading: boolean }>()
const emit = defineEmits<{ 'update:selectedWindow': [value: ActiveNowWindow['id']] }>()
const { t } = useI18n()
const windows = props.windows
const current = computed(() => props.windows.find((window) => window.id === props.selectedWindow) || props.windows[0])
const label = (id: ActiveNowWindow['id']) => id === '60m' ? t('accountInsights.active.sixtyMinutes') : id === '12h' ? t('accountInsights.active.twelveHours') : t('accountInsights.active.twentyFourHours')
const format = (value: number | null) => {
  if (value === null || !Number.isFinite(value)) return '—'
  if (value === 0) return '0'
  const magnitude = Math.abs(value)
  if (magnitude < 0.01) return '<0.01'
  if (magnitude < 1) return value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
  return Math.round(value).toLocaleString()
}
const relative = (value: string) => { const minutes = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 60000)); return minutes < 60 ? `${minutes}m` : `${Math.floor(minutes / 60)}h` }
const Metric = (props: { label: string; value: string }) => h('div', { class: 'rounded border border-gray-200 p-3 dark:border-dark-700' }, [h('div', { class: 'text-[11px] text-gray-500' }, props.label), h('div', { class: 'mt-1 text-sm font-semibold text-gray-900 dark:text-white' }, props.value)])
</script>
