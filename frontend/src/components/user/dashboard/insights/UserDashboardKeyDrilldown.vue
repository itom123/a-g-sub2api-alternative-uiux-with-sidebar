<template>
  <section class="card min-w-0 p-4" data-testid="account-key-drilldown">
    <div class="mb-4 flex items-center justify-between gap-2">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.drilldown.title') }}{{ keyInfo ? ` · ${keyInfo.name}` : '' }}</h3>
      <button type="button" class="btn btn-secondary px-2 py-1 text-xs" @click="emit('close')">{{ t('accountInsights.keys.closeDetails') }}</button>
    </div>
    <div class="grid min-w-0 gap-5 lg:grid-cols-1" data-testid="key-drilldown-panels">
      <div class="min-w-0" data-testid="key-daily-usage">
        <h4 class="mb-2 text-xs font-semibold text-gray-700 dark:text-gray-300">{{ t('accountInsights.drilldown.dailyUsage') }}</h4>
        <LoadingSpinner v-if="dailyLoadingState" class="mx-auto" />
        <p v-else-if="dailyError" role="alert" class="py-6 text-sm text-red-600 dark:text-red-400">{{ t('accountInsights.drilldown.unavailable') }}</p>
        <template v-else-if="daily?.items?.length">
          <div class="hidden max-w-full overflow-x-auto md:block"><table class="w-full table-fixed text-xs"><thead><tr class="text-left text-gray-500"><th class="py-1">{{ t('accountInsights.csv.range') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.dailyRequests') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.dailyTokens') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.input') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.cacheCreation') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.cacheRead') }}</th><th class="py-1 text-right">{{ t('accountInsights.drilldown.output') }}</th><th class="py-1 text-right">{{ t('accountInsights.charts.actual') }}</th></tr></thead><tbody><tr v-for="item in daily.items" :key="item.date" class="border-t border-gray-100 dark:border-dark-700"><td class="py-1">{{ item.date }}</td><td class="py-1 text-right">{{ item.requests }}</td><td class="py-1 text-right">{{ item.total_tokens }}</td><td class="py-1 text-right">{{ item.input_tokens }}</td><td class="py-1 text-right">{{ item.cache_write_tokens }}</td><td class="py-1 text-right">{{ item.cache_read_tokens }}</td><td class="py-1 text-right">{{ item.output_tokens }}</td><td class="py-1 text-right">{{ money(item.actual_cost) }}</td></tr></tbody></table></div>
          <div class="space-y-3 text-xs md:hidden">
            <dl v-for="item in daily.items" :key="item.date" class="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1 border-t border-gray-100 pt-2 dark:border-dark-700">
              <dt class="font-medium text-gray-500 dark:text-gray-400">{{ t('accountInsights.csv.range') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.date }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.dailyRequests') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.requests }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.dailyTokens') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.total_tokens }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.input') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.input_tokens }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.cacheCreation') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.cache_write_tokens }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.cacheRead') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.cache_read_tokens }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.drilldown.output') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ item.output_tokens }}</dd>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('accountInsights.charts.actual') }}</dt><dd class="text-right tabular-nums text-gray-900 dark:text-white">{{ money(item.actual_cost) }}</dd>
            </dl>
          </div>
        </template>
        <p v-else class="py-6 text-sm text-gray-500 dark:text-gray-400">{{ t('accountInsights.empty.noUsage') }}</p>
      </div>
      <div class="min-w-0" data-testid="key-model-split">
        <h4 class="mb-2 text-xs font-semibold text-gray-700 dark:text-gray-300">{{ t('accountInsights.drilldown.modelSplit') }}</h4>
        <LoadingSpinner v-if="modelsLoadingState" class="mx-auto" />
        <p v-else-if="modelsError" role="alert" class="py-6 text-sm text-red-600 dark:text-red-400">{{ t('accountInsights.drilldown.unavailable') }}</p>
        <div v-else-if="models.length" class="space-y-2"><div v-for="model in models" :key="model.model" class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 text-xs"><span class="min-w-0 break-words text-gray-700 dark:text-gray-300">{{ model.model }}</span><span class="shrink-0 whitespace-nowrap tabular-nums text-gray-500">{{ model.requests }} · {{ money(model.actual_cost) }}</span></div></div>
        <p v-else class="py-6 text-sm text-gray-500 dark:text-gray-400">{{ t('accountInsights.common.noData') }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { cnyNumber } from '@/utils/currency'
import type { ApiKey, ModelStat } from '@/types'
import type { ApiKeyDailyUsageResponse } from '@/api/usage'

const props = withDefaults(defineProps<{
  keyInfo: ApiKey | null
  daily: ApiKeyDailyUsageResponse | null
  models: ModelStat[]
  loading: boolean
  dailyLoading?: boolean
  modelsLoading?: boolean
  dailyError?: boolean
  modelsError?: boolean
}>(), {
  dailyLoading: false,
  modelsLoading: false,
  dailyError: false,
  modelsError: false,
})
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const dailyLoadingState = computed(() => props.loading || props.dailyLoading)
const modelsLoadingState = computed(() => props.loading || props.modelsLoading)
const money = (value: number | null | undefined) => {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '—'
  const number = cnyNumber(amount)
  return number === '-' ? `$${amount.toFixed(4)}` : `¥${number}`
}
</script>
