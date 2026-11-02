<template>
  <span data-testid="health-chip" class="inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs font-medium" :class="classes">
    <span aria-hidden="true">{{ icon }}</span><span>{{ label }}</span>
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DashboardHealth } from '../insights/types'
const props = defineProps<{ health: DashboardHealth }>()
const { t } = useI18n()
const label = computed(() => t(`accountInsights.health.${props.health === 'all_good' ? 'allGood' : props.health === 'attention' ? 'attention' : 'awaitingData'}`))
const icon = computed(() => props.health === 'all_good' ? '✓' : props.health === 'attention' ? '!' : '…')
const classes = computed(() => props.health === 'all_good' ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200' : props.health === 'attention' ? 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-200' : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200')
</script>
