<template>
  <section class="card p-4" data-testid="rolling-30d"><div class="flex flex-wrap items-baseline justify-between gap-2"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountInsights.rolling.title') }}</h3><span class="text-xs text-gray-500">{{ t('accountInsights.rolling.subtitle') }}</span></div><div class="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-6"><Stat :label="t('accountInsights.rolling.spend')" :value="money(summary.actualCostUsd)" /><Stat :label="t('accountInsights.rolling.requests')" :value="format(summary.requests)" /><Stat :label="t('accountInsights.rolling.tokens')" :value="format(summary.totalTokens)" /><Stat :label="t('accountInsights.rolling.activeDays')" :value="format(summary.activeDays)" /><Stat :label="t('accountInsights.consistency.callsPerActiveDay')" :value="callsPerActiveDay" /><Stat :label="t('accountInsights.rolling.tokensPerCall')" :value="format(summary.tokensPerCall)" /></div></section>
</template>
<script setup lang="ts">
import { computed, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatCny } from '@/utils/currency'
import type { DashboardRolling30d } from '../insights/types'
const props = defineProps<{ summary: DashboardRolling30d; loading: boolean }>()
const callsPerActiveDay = computed(() => props.summary.requests == null || props.summary.activeDays == null ? '—' : props.summary.activeDays > 0 ? Math.round(props.summary.requests / props.summary.activeDays).toLocaleString() : '0')
const { t } = useI18n()
function format(v: number | null): string { return v == null || !Number.isFinite(v) ? '—' : Math.round(Number(v)).toLocaleString() }
function money(v: number | null): string { if (v == null || !Number.isFinite(Number(v))) return '—'; const value = formatCny(v, 2); return value === '-' ? '—' : value }
const Stat = defineComponent({ props: { label: String, value: String }, setup(p) { return () => h('div', [h('p', { class: 'text-xs text-gray-500' }, p.label), h('p', { class: 'mt-1 font-semibold tabular-nums text-gray-900 dark:text-white' }, p.value)]) } })
</script>
