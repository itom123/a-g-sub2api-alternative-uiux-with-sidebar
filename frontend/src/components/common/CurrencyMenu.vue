<template>
  <button type="button" class="btn-ghost btn-icon" :title="t('common.currency.title')" :aria-label="t('common.currency.title')" @click="show = true">
    <Icon name="creditCard" size="sm" />
  </button>
  <BaseDialog :show="show" :title="t('common.currency.title')" width="narrow" @close="show = false">
    <dl v-if="currencyRate" class="space-y-3 text-sm">
      <div class="flex flex-wrap justify-between gap-2"><dt>{{ t('common.currency.rate') }}</dt><dd class="font-semibold tabular-nums">1 USD = ¥{{ currencyRate.cnyPerUsd.toFixed(6) }}</dd></div>
      <div class="flex flex-wrap justify-between gap-2"><dt>{{ t('common.currency.source') }}</dt><dd>ECB · {{ currencyRate.rateDate }}</dd></div>
      <div class="flex flex-wrap justify-between gap-2"><dt>{{ t('common.currency.updated') }}</dt><dd>{{ shanghaiTime(currencyRate.updatedAt) }}</dd></div>
      <div class="flex flex-wrap justify-between gap-2"><dt>{{ t('common.currency.schedule') }}</dt><dd>{{ t('common.currency.weekly') }}</dd></div>
      <p v-if="Date.now() > Date.parse(currencyRate.nextRefreshAt)" class="text-amber-700 dark:text-amber-300">{{ t('common.currency.stale') }}</p>
    </dl>
    <p v-else class="text-sm text-amber-700 dark:text-amber-300">{{ t('common.currency.unavailable') }}</p>
    <a :href="buildApiUrl('/currency')" target="_blank" rel="noopener noreferrer" class="mt-4 inline-flex items-center gap-2 text-sm text-primary-600">
      <Icon name="externalLink" size="sm" />{{ t('common.currency.api') }}
    </a>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { currencyRate } from '@/utils/currency'
import { buildApiUrl } from '@/api/url'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
const { t } = useI18n()
const show = ref(false)
const shanghaiTime = (value: string) => new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
</script>
