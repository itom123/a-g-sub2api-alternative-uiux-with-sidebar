<template>
  <section class="card min-w-0 overflow-hidden p-4" data-testid="account-errors">
    <div class="mb-4"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.errors.title') }}</h3><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.subtitle') }}</p></div>
    <LoadingSpinner v-if="loading" class="mx-auto" />
    <p v-else-if="unavailable" class="py-6 text-sm text-amber-700 dark:text-amber-300">{{ t('accountInsights.errors.unavailable') }}</p>
    <p v-else-if="!items.length" class="py-6 text-sm text-gray-500">{{ t('accountInsights.errors.noErrors') }}</p>
    <div v-else>
      <div class="space-y-3 md:hidden">
        <article v-for="item in items" :key="item.id" class="cursor-pointer rounded-lg border border-gray-100 p-3 dark:border-dark-700/70" @click="emit('select-error', item.id)">
          <dl class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.time') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ formatDate(item.created_at) }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.model') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ item.model }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.endpoint') }}</dt><dd class="min-w-0 break-words [overflow-wrap:anywhere] text-right text-gray-900 dark:text-white">{{ item.inbound_endpoint }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.status') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ item.status_code }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.category') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ item.category }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.key') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ item.key_name }}</dd>
            <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.errors.message') }}</dt><dd class="min-w-0 break-words text-right text-gray-900 dark:text-white">{{ item.message }}</dd>
          </dl>
        </article>
      </div>
      <div class="hidden overflow-hidden md:block">
        <table class="w-full table-fixed text-xs">
          <colgroup><col class="min-w-[8rem] w-[16%]" /><col class="w-[18%]" /><col class="min-w-[10rem] w-[18%]" /><col class="min-w-[3.5rem] w-[9%]" /><col class="min-w-[7rem] w-[13%]" /><col class="min-w-[9rem] w-[12%]" /><col class="min-w-[7rem] w-[14%]" /></colgroup>
          <thead><tr class="border-b border-gray-200 text-left text-gray-500 dark:border-dark-700"><th v-for="(label, index) in labels" :key="label" :class="['whitespace-nowrap px-3 py-2 font-medium', index === 3 ? 'pr-4' : '', index === 4 ? 'pl-4' : '']">{{ label }}</th></tr></thead>
          <tbody><tr v-for="item in items" :key="item.id" class="cursor-pointer border-b border-gray-100 dark:border-dark-700/70" @click="emit('select-error', item.id)"><td class="min-w-[8rem] whitespace-nowrap px-3 py-2">{{ formatDate(item.created_at) }}</td><td class="break-words px-3 py-2 [overflow-wrap:break-word]">{{ item.model }}</td><td class="whitespace-nowrap px-3 py-2">{{ item.inbound_endpoint }}</td><td class="min-w-[3.5rem] whitespace-nowrap px-3 py-2">{{ item.status_code }}</td><td class="whitespace-nowrap px-3 py-2 [overflow-wrap:anywhere]">{{ item.category }}</td><td class="whitespace-nowrap px-3 py-2 [overflow-wrap:anywhere]">{{ item.key_name }}</td><td class="min-w-0 whitespace-normal break-words px-3 py-2 [overflow-wrap:break-word]" :title="item.message">{{ item.message }}</td></tr></tbody>
        </table>
      </div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DashboardErrorRow } from './types'
defineProps<{ items: DashboardErrorRow[]; loading: boolean; unavailable: boolean }>()
const emit = defineEmits<{ 'select-error': [id: number] }>()
const { t } = useI18n()
const labels = [
  t('accountInsights.errors.time'), t('accountInsights.errors.model'), t('accountInsights.errors.endpoint'),
  t('accountInsights.errors.status'), t('accountInsights.errors.category'), t('accountInsights.errors.key'), t('accountInsights.errors.message'),
]
const formatDate = (value: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
</script>
