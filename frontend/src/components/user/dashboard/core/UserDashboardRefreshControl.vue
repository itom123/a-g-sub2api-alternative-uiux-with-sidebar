<template>
  <div class="flex w-full flex-wrap items-center justify-start gap-2 sm:w-auto" data-testid="refresh-control">
    <label class="text-xs text-gray-500" for="account-refresh-interval">{{ t('accountInsights.refreshInterval.label') }}</label>
    <select id="account-refresh-interval" class="input" :value="seconds" @change="emit('update:seconds', Number(($event.target as HTMLSelectElement).value))" aria-label="Refresh interval">
      <option :value="0">{{ t('accountInsights.refreshInterval.off') }}</option>
      <option :value="30">{{ t('accountInsights.refreshInterval.thirtySeconds') }}</option>
      <option :value="60">{{ t('accountInsights.refreshInterval.oneMinute') }}</option>
      <option :value="300">{{ t('accountInsights.refreshInterval.fiveMinutes') }}</option>
      <option :value="900">{{ t('accountInsights.refreshInterval.fifteenMinutes') }}</option>
    </select>
    <span v-if="seconds > 0 && hidden" class="text-xs text-gray-500">{{ t('accountInsights.refreshInterval.pausedHidden') }}</span>
    <span v-else-if="seconds > 0" class="text-xs text-gray-500">{{ t('accountInsights.refreshInterval.nextRefresh', { seconds: countdown }) }}</span>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ seconds: number; countdown: number; hidden?: boolean }>()
const emit = defineEmits<{ 'update:seconds': [value: number] }>()
const { t } = useI18n()
</script>
