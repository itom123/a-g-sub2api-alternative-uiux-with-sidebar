<template>
  <section class="card p-4" data-testid="account-consistency">
    <div class="mb-4"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.consistency.title') }}</h3><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountInsights.consistency.subtitle') }}</p></div>
    <LoadingSpinner v-if="loading" class="mx-auto" />
    <div v-else-if="summary" class="grid grid-cols-2 gap-3 sm:grid-cols-4"><Metric :label="t('accountInsights.consistency.requests')" :value="display(summary.requests)" /><Metric :label="t('accountInsights.consistency.activeDays')" :value="display(summary.activeDays30)" /><Metric :label="t('accountInsights.consistency.callsPerActiveDay')" :value="display(summary.callsPerActiveDay)" /><Metric :label="t('accountInsights.consistency.tokensPerCall')" :value="display(summary.tokensPerCall)" /><Metric :label="t('accountInsights.consistency.sevenDayTrend')" :value="trendLabel" /></div>
    <p v-else class="text-sm text-gray-500">{{ t('accountInsights.consistency.noData') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, h } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DashboardConsistency } from './types'
const props = defineProps<{ summary: DashboardConsistency | null; loading: boolean }>()
const { t } = useI18n()
const display = (value: number | null | undefined) => value === null || value === undefined || !Number.isFinite(value) ? '—' : Math.round(value).toLocaleString()
const trendLabel = computed(() => !props.summary || props.summary.trend7d === 'no_data' ? t('accountInsights.consistency.noData') : t(`accountInsights.consistency.${props.summary.trend7d}`))
const Metric = (props: { label: string; value: string }) => h('div', { class: 'rounded border border-gray-200 p-3 dark:border-dark-700' }, [h('div', { class: 'text-[11px] text-gray-500' }, props.label), h('div', { class: 'mt-1 text-sm font-semibold text-gray-900 dark:text-white' }, props.value)])
</script>
