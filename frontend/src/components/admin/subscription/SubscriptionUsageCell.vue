<template>
  <div class="subscription-usage-cell" :data-period="window.period" :data-state="window.state">
    <div class="subscription-usage-cell__amount">
      <span data-test="usage-used">{{ window.used === null ? t('common.notAvailable') : formatSubscriptionMoney(window.used) }}</span>
      <span class="subscription-usage-cell__separator" aria-hidden="true">/</span>
      <span data-test="usage-limit">{{ limitLabel }}</span>
    </div>
    <div class="subscription-usage-cell__window">
      <span v-if="resetText" class="subscription-usage-cell__reset">{{ resetText }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatSubscriptionMoney, type SubscriptionQuotaWindow } from '@/utils/subscriptionQuota'

const props = defineProps<{
  window: SubscriptionQuotaWindow
  resetText?: string
}>()

const { t } = useI18n()

const limitLabel = computed(() => {
  if (props.window.state === 'unlimited') return t('admin.subscriptions.unlimited')
  if (props.window.state === 'finite' && props.window.limit !== null) {
    return formatSubscriptionMoney(props.window.limit)
  }
  return t('common.notAvailable')
})
</script>

<style scoped>
.subscription-usage-cell {
  @apply w-full text-right;
}

.subscription-usage-cell__amount {
  @apply flex items-baseline justify-end gap-1 whitespace-nowrap text-[13px] font-medium tabular-nums text-gray-700 dark:text-gray-200;
  line-height: 1.25rem;
}

.subscription-usage-cell__separator {
  @apply font-normal text-gray-400 dark:text-gray-500;
}

.subscription-usage-cell__window {
  @apply whitespace-nowrap text-[10px] tabular-nums text-gray-500 dark:text-gray-400;
  min-height: 1rem;
  line-height: 1rem;
}
</style>
