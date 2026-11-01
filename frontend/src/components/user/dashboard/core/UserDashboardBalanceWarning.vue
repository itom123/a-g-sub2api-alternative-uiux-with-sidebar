<template>
  <section v-if="visible && !dismissed" class="rounded-xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100" data-testid="balance-warning">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div><h3 class="font-semibold">{{ t('accountInsights.warning.title') }}</h3><p class="mt-1">{{ t('accountInsights.warning.description') }}</p><div class="mt-2 flex items-center gap-2 text-xs"><label for="account-warning-threshold">{{ t('accountInsights.warning.thresholdLabel') }}</label><input id="account-warning-threshold" v-model.number="threshold" class="input w-24" type="number" min="0" step="1" @change="saveThreshold" /><button type="button" class="underline" @click="resetThreshold">{{ t('accountInsights.warning.reset') }}</button></div></div>
      <div class="flex gap-2"><button type="button" class="btn btn-primary" @click="topUp">{{ t('accountInsights.warning.topUp') }}</button><button type="button" class="btn btn-secondary" @click="dismiss">{{ t('accountInsights.warning.dismiss') }}</button></div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import type { User } from '@/types'
const props = defineProps<{ profile: User; visible: boolean; thresholdCny: number; dismissed: boolean }>()
const { t } = useI18n(); const router = useRouter(); const appStore = useAppStore()
const threshold = ref(props.thresholdCny)
function key() { return `account-insights:warning-dismissed:${props.profile.id}` }
function dismiss() { localStorage.setItem(key(), '1'); window.dispatchEvent(new Event('account-insights-warning-dismissed')) }
function saveThreshold() { if (Number.isFinite(threshold.value) && threshold.value >= 0) localStorage.setItem(`account-insights:warning-threshold:${props.profile.id}`, String(threshold.value)) }
function resetThreshold() { threshold.value = 210; saveThreshold() }
async function topUp() {
  try {
    if (!appStore.publicSettingsLoaded) await appStore.fetchPublicSettings()
    if (appStore.publicSettingsLoaded && appStore.cachedPublicSettings?.payment_enabled === false) { await router.push('/redeem'); return }
    await router.push('/purchase')
  } catch { await router.push('/redeem') }
}
</script>
