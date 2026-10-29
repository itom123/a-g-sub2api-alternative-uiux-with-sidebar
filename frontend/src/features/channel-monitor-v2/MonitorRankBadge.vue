<template>
  <span
    class="inline-flex items-center gap-1.5 tabular-nums"
    :class="rankClass"
    :title="titleText"
  >
    <Icon v-if="showTrophy" name="trophy" class="rank-trophy shrink-0" aria-hidden="true" />
    <span v-if="showTrophy" class="sr-only">{{ ariaLabel }}</span>
    <span
      class="text-xs font-semibold"
      :class="showTrophy ? rankClass : 'text-gray-500 dark:text-dark-300'"
    >
      {{ label }}
    </span>
  </span>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  rank: number | null | undefined
}>()

const rankNum = computed(() => {
  const n = Number(props.rank)
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : null
})

const showTrophy = computed(
  () => rankNum.value != null && rankNum.value >= 1 && rankNum.value <= 3,
)


const label = computed(() => {
  // Rank 0 = present but unranked (no traffic in window / outside top list).
  if (rankNum.value == null || rankNum.value <= 0) return '—'
  return `#${rankNum.value}`
})

const ariaLabel = computed(() => {
  if (rankNum.value == null || rankNum.value <= 0) return t('channelMonitorV2.rank.unranked')
  if (rankNum.value === 1) return t('channelMonitorV2.rank.gold')
  if (rankNum.value === 2) return t('channelMonitorV2.rank.silver')
  if (rankNum.value === 3) return t('channelMonitorV2.rank.bronze')
  return t('channelMonitorV2.rank.place', { n: rankNum.value })
})

const titleText = computed(() => ariaLabel.value)

const rankClass = computed(() => {
  if (rankNum.value === 1) return 'text-amber-600 dark:text-amber-400'
  if (rankNum.value === 2) return 'text-slate-500 dark:text-slate-300'
  if (rankNum.value === 3) return 'text-amber-800 dark:text-amber-600'
  return ''
})
</script>
