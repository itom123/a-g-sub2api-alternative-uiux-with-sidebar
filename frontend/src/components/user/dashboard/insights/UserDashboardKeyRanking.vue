<template>
  <section class="card min-w-0 overflow-hidden p-4 xl:col-span-2" data-testid="account-key-ranking">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.keys.title') }}</h3>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.keys.subtitle') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <span v-if="truncated" class="text-xs text-amber-700 dark:text-amber-300">{{ t('accountInsights.keys.rowsCapped') }}</span>
        <button type="button" class="btn btn-secondary whitespace-normal px-2 py-1 text-xs" @click="emit('export-csv')">{{ t('accountInsights.csv.export') }}</button>
      </div>
    </div>
    <LoadingSpinner v-if="loading" class="mx-auto" />
    <div v-else-if="rows.length" class="overflow-visible">
      <table class="block w-full md:table md:table-fixed text-xs">
        <colgroup class="hidden md:table-column-group">
          <col v-for="column in columns" :key="column.key" :style="{ width: column.width }" />
        </colgroup>
        <thead class="hidden md:table-header-group">
          <tr class="border-b border-gray-200 text-left text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <th v-for="column in columns" :key="column.key" class="break-words px-2 py-2 font-medium">
              <button v-if="column.sort" type="button" class="break-words text-left hover:text-gray-900 dark:hover:text-white" @click="emit('sort-change', column.sort)">{{ column.label }}</button>
              <span v-else>{{ column.label }}</span>
            </th>
          </tr>
        </thead>
        <tbody class="block md:table-row-group">
          <tr v-for="row in rows" :key="row.apiKeyId" class="mb-3 block rounded-lg md:table-row border border-gray-100 p-2 last:mb-0 dark:border-dark-700/70 md:mb-0 md:rounded-none md:border-x-0 md:border-b md:border-t-0 md:p-0" :class="selectedKeyId === row.apiKeyId ? 'bg-gray-50 dark:bg-dark-800/80' : ''">
            <td data-label="Name" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 font-medium text-gray-900 before:min-w-0 before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:text-white dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words text-right md:text-left">{{ row.keyName }}</span></td>
            <td data-label="Status" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 before:min-w-0 before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words text-right md:text-left">{{ row.status }}</span></td>
            <td :data-label="t('accountInsights.keys.rangeCost')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 text-right before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words">{{ money(row.selectedRangeCostUsd) }}</span></td>
            <td :data-label="t('accountInsights.keys.lifetimeCost')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 text-right before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words">{{ money(row.lifetimeCostUsd) }}</span></td>
            <td :data-label="t('accountInsights.keys.calls')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 text-right before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words">{{ row.requests ?? '—' }}</span></td>
            <td :data-label="t('accountInsights.keys.tokens')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 text-right before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words">{{ row.totalTokens ?? '—' }}</span></td>
            <td :data-label="t('accountInsights.keys.favouriteModel')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="min-w-0 break-words text-right md:text-left">{{ row.favouriteModel || t('accountInsights.keys.noFavouriteModel') }}</span></td>
            <td :data-label="t('accountInsights.keys.lastUse')" class="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] items-start gap-3 px-2 py-2 before:min-w-0 before:text-left before:font-medium before:text-gray-500 before:content-[attr(data-label)] dark:before:text-gray-400 md:table-cell md:before:content-none"><span class="break-words text-right md:text-left">{{ row.lastUsedAt ? formatDate(row.lastUsedAt) : t('accountInsights.keys.neverUsed') }}</span></td>
            <td data-label="" class="flex min-w-0 justify-end px-2 py-2 md:table-cell"><button type="button" class="btn btn-secondary whitespace-normal px-2 py-1 text-xs" @click="emit('select-key', row.apiKeyId)">{{ t('accountInsights.keys.openDetails') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('accountInsights.keys.noKeys') }}</p>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { cnyNumber } from '@/utils/currency'
import type { DashboardKeyUsageRow } from './types'

defineProps<{ rows: DashboardKeyUsageRow[]; loading: boolean; selectedKeyId: number | null; truncated: boolean }>()
const emit = defineEmits<{ 'select-key': [apiKeyId: number]; 'sort-change': [key: 'cost' | 'calls' | 'tokens' | 'favouriteModel' | 'lastUsedAt']; 'export-csv': [] }>()
const { t } = useI18n()
const columns = [
  { key: 'name', label: t('accountInsights.keys.name'), width: '16%' },
  { key: 'status', label: t('accountInsights.keys.status'), width: '9%' },
  { key: 'cost', label: t('accountInsights.keys.rangeCost'), width: '11%', sort: 'cost' as const },
  { key: 'lifetime', label: t('accountInsights.keys.lifetimeCost'), width: '11%' },
  { key: 'calls', label: t('accountInsights.keys.calls'), width: '9%', sort: 'calls' as const },
  { key: 'tokens', label: t('accountInsights.keys.tokens'), width: '10%', sort: 'tokens' as const },
  { key: 'model', label: t('accountInsights.keys.favouriteModel'), width: '14%', sort: 'favouriteModel' as const },
  { key: 'last', label: t('accountInsights.keys.lastUse'), width: '12%', sort: 'lastUsedAt' as const },
  { key: 'open', label: '', width: '8%' },
]
const money = (value: number | null) => value === null ? '—' : `¥${cnyNumber(value)}`
const formatDate = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
</script>
