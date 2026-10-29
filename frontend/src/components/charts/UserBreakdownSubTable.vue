<template>
  <!-- Rows belong to the parent table so every value sits under its own column heading. -->
  <tr v-if="loading" class="ag-breakdown-row border-t border-gray-100/50 dark:border-dark-700/50">
    <td :colspan="columnCount" class="py-3 text-center">
      <LoadingSpinner />
    </td>
  </tr>
  <tr v-else-if="items.length === 0" class="ag-breakdown-row border-t border-gray-100/50 dark:border-dark-700/50">
    <td :colspan="columnCount" class="py-2 text-center text-xs text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </td>
  </tr>
  <template v-else>
    <tr
      v-for="user in items"
      :key="user.user_id"
      class="ag-breakdown-row border-t border-gray-100/50 dark:border-dark-700/50"
    >
      <td
        :colspan="labelColspan > 1 ? labelColspan : undefined"
        class="max-w-0 truncate py-1 pl-6 text-gray-600 dark:text-gray-300"
        :title="user.email"
      >
        {{ user.email || `User #${user.user_id}` }}
      </td>
      <td class="py-1 text-right text-gray-500 dark:text-gray-400" :data-label="t('admin.dashboard.requests')">
        {{ user.requests.toLocaleString() }}
      </td>
      <td class="py-1 text-right text-gray-500 dark:text-gray-400" :data-label="t('admin.dashboard.tokens')">
        {{ formatTokens(user.total_tokens) }}
      </td>
      <td class="py-1 text-right text-green-600 dark:text-green-400" :data-label="t('admin.dashboard.actual')">
        ¥{{ formatCost(user.actual_cost) }}
      </td>
      <td v-if="showAccountCost" class="py-1 text-right text-orange-500 dark:text-orange-400" :data-label="t('admin.dashboard.accountCost')">
        ¥{{ formatCost(user.account_cost) }}
      </td>
      <td class="py-1 text-right text-gray-400 dark:text-gray-500" :data-label="t('admin.dashboard.standard')">
        ¥{{ formatCost(user.cost) }}
      </td>
    </tr>
  </template>
</template>

<script setup lang="ts">
import { cnyCost } from '@/utils/currency'

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { UserBreakdownItem } from '@/types'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  items: UserBreakdownItem[]
  loading?: boolean
  showAccountCost?: boolean
  labelColspan?: number
}>(), {
  loading: false,
  showAccountCost: true,
  labelColspan: 1,
})

const showAccountCost = computed(() => props.showAccountCost)
const columnCount = computed(() => props.labelColspan + (props.showAccountCost ? 5 : 4))

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number | undefined | null): string => {
  return cnyCost(value)
}
</script>
