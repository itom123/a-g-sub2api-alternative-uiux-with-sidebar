<template>
  <section
    v-if="featureEnabled && visible"
    class="dashboard-subscriptions"
    data-testid="dashboard-subscriptions"
    :aria-label="t('subscriptionProgress.title')"
    :aria-busy="subscriptionStore.loading"
  >
    <header class="dashboard-subscriptions-heading">
      <h2>{{ t('subscriptionProgress.title') }}</h2>
      <button type="button" :disabled="subscriptionStore.loading" @click="refresh">{{ t('subscriptionProgress.refresh') }}</button>
    </header>
    <p class="dashboard-subscriptions-note">{{ t('subscriptionProgress.independentQuotas') }}</p>
    <p v-if="subscriptionStore.error" role="status" class="dashboard-subscriptions-status" data-testid="subscription-refresh-error">
      {{ t(`subscriptionProgress.${subscriptionStore.loaded ? 'refreshError' : 'loadError'}`) }}
      <button type="button" :disabled="subscriptionStore.loading" @click="refresh">{{ t('subscriptionProgress.retry') }}</button>
    </p>
    <p v-else-if="!subscriptionStore.loaded" role="status" class="dashboard-subscriptions-status">{{ t('subscriptionProgress.loading') }}</p>
    <div class="dashboard-subscription-plans">
      <article v-for="plan in subscriptionStore.allowances" :key="plan.id" class="dashboard-subscription-plan" :data-plan-id="plan.id">
        <div class="dashboard-subscription-plan-heading">
          <h3>{{ plan.name || t('subscriptionProgress.planFallback', { id: plan.groupId }) }}</h3>
          <p>{{ expiryLabel(plan.expiresAt) }}</p>
        </div>
        <dl class="dashboard-subscription-windows">
          <div v-for="window in plan.windows" :key="window.period" class="dashboard-subscription-window" :data-period="window.period">
            <dt>{{ t(`subscriptionProgress.${window.oneTime ? 'oneTime' : window.period}`) }}</dt>
            <dd>
              <span class="dashboard-subscription-remaining">
                <span>{{ t('subscriptionProgress.remaining') }}</span>
                <strong>{{ window.state === 'finite' ? money(window.remaining!) : t(`subscriptionProgress.${window.state}`) }}</strong>
              </span>
              <span class="dashboard-subscription-usage">
                {{ t('subscriptionProgress.used') }} {{ window.used === null ? t('subscriptionProgress.unknown') : money(window.used) }}
                / {{ t('subscriptionProgress.limit') }} {{ window.state === 'finite' ? money(window.limit!) : t(`subscriptionProgress.${window.state}`) }}
              </span>
            </dd>
          </div>
        </dl>
      </article>
    </div>
    <p v-if="subscriptionStore.lastFetchedAt !== null" class="dashboard-subscriptions-updated">
      {{ t('subscriptionProgress.updatedAt', { date: dateTime(subscriptionStore.lastFetchedAt) }) }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSubscriptionStore } from '@/stores'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { formatSubscriptionMoney as money, getExpirationDateRelation } from '@/utils/subscriptionQuota'

const { t, locale } = useI18n()
const subscriptionStore = useSubscriptionStore()
const featureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))
// A confirmed empty snapshot is not a dashboard panel; unknown/error is still useful.
const visible = computed(() => !subscriptionStore.loaded || Boolean(subscriptionStore.error) || subscriptionStore.allowances.length > 0)
function dateTime(value: number | string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('subscriptionProgress.unknown')
  return date.toLocaleString(locale.value, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
function expiryLabel(expiresAt: string | null) {
  if (!expiresAt) return t('subscriptionProgress.noExpiry')
  const relation = getExpirationDateRelation(expiresAt, new Date(subscriptionStore.currentTime))
  if (!relation) return t('subscriptionProgress.unknown')
  return t('subscriptionProgress.expiresAt', { date: dateTime(expiresAt) })
}
function refresh() {
  if (featureEnabled.value) subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
}
watch(featureEnabled, (enabled) => {
  if (enabled) subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
onMounted(() => {
  if (featureEnabled.value) subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
</script>

<style scoped>
.dashboard-subscriptions { min-width: 0; border: 1px solid var(--ag-rule, currentColor); padding: 1rem; color: var(--ag-ink, #000); background: var(--ag-paper, #fff); font-family: var(--ag-font, inherit); }
.dashboard-subscriptions-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 1rem; }
.dashboard-subscriptions-heading h2 { font-size: 0.875rem; font-weight: 600; }
.dashboard-subscriptions button { font-size: 0.75rem; text-decoration: underline; text-underline-offset: 3px; }
.dashboard-subscriptions button:disabled { opacity: 0.5; cursor: wait; }
.dashboard-subscriptions button:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
.dashboard-subscriptions-note, .dashboard-subscriptions-status, .dashboard-subscriptions-updated { margin-top: 0.65rem; font-size: 0.75rem; line-height: 1.5; }
.dashboard-subscriptions-status { border-top: 1px solid var(--ag-rule, currentColor); padding-top: 0.65rem; }
.dashboard-subscription-plans { display: grid; }
.dashboard-subscription-plan { min-width: 0; margin-top: 1rem; border-top: 1px solid var(--ag-rule, currentColor); padding-top: 1rem; }
.dashboard-subscription-plan-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 0.75rem; flex-wrap: wrap; }
.dashboard-subscription-plan-heading h3 { font-size: 0.875rem; font-weight: 600; overflow-wrap: anywhere; }
.dashboard-subscription-plan-heading p { font-size: 0.75rem; }
.dashboard-subscription-windows { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1rem; margin-top: 1rem; }
.dashboard-subscription-window { min-width: 0; }
.dashboard-subscription-window dt { font-size: 0.75rem; line-height: 1.5; }
.dashboard-subscription-window dd { display: flex; flex-direction: column; gap: 0.35rem; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.dashboard-subscription-remaining { display: flex; align-items: baseline; flex-wrap: wrap; gap: 0.35rem; }
.dashboard-subscription-remaining > span { font-size: 0.75rem; }
.dashboard-subscription-remaining strong { font-size: 1.25rem; font-weight: 600; line-height: 1.5; }
.dashboard-subscription-usage { font-size: 0.75rem; line-height: 1.5; }
@media (max-width: 639px) {
  .dashboard-subscription-windows { grid-template-columns: minmax(0, 1fr); gap: 0.75rem; }
  .dashboard-subscription-window { display: grid; grid-template-columns: 5rem minmax(0, 1fr); gap: 0.75rem; }
  .dashboard-subscription-window dd { text-align: right; }
  .dashboard-subscription-remaining { justify-content: flex-end; }
}
</style>
