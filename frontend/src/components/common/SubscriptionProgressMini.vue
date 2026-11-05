<template>
  <div v-if="subscriptionFeatureEnabled && visible" ref="containerRef" class="subscription-mini" data-testid="subscription-mini" @focusout="handleFocusOut">
    <button
      ref="triggerRef"
      type="button"
      class="subscription-mini-trigger"
      :title="t('subscriptionProgress.viewDetails')"
      :aria-label="triggerLabel"
      :aria-expanded="tooltipOpen"
      aria-controls="header-subscription-details"
      aria-haspopup="dialog"
      @click="toggleTooltip"
    >
      <span class="subscription-mini-name">{{ selected ? planName(selected) : t('subscriptionProgress.title') }}</span>
      <span class="subscription-mini-value" data-testid="subscription-mini-remaining">
        <template v-if="selected">
          <span v-if="selected.state === 'finite' && selected.limitingWindow">
            {{ periodLabel(selected.limitingWindow) }} {{ t('subscriptionProgress.remaining') }} {{ money(selected.limitingWindow.remaining!) }}
          </span>
          <span v-else>{{ t(`subscriptionProgress.${selected.state === 'unlimited' ? 'unlimited' : 'unknown'}`) }}</span>
          <span v-if="plans.length > 1" class="subscription-mini-more">{{ t('subscriptionProgress.morePlans', { count: plans.length - 1 }) }}</span>
        </template>
        <template v-else>{{ t(`subscriptionProgress.${subscriptionStore.error ? 'unknown' : 'loading'}`) }}</template>
      </span>
    </button>

    <div
      v-if="tooltipOpen"
      id="header-subscription-details"
      class="subscription-popover"
      :style="{ '--subscription-popover-top': `${popoverTop}px` }"
      role="dialog"
      :aria-label="t('subscriptionProgress.title')"
      tabindex="-1"
    >
      <div class="subscription-popover-heading">
        <h3>{{ t('subscriptionProgress.title') }}</h3>
        <button type="button" class="subscription-close" :aria-label="t('subscriptionProgress.close')" @click="closeTooltip(true)">×</button>
      </div>
      <p class="subscription-popover-note">{{ t('subscriptionProgress.independentQuotas') }}</p>
      <p v-if="subscriptionStore.error" role="status" class="subscription-popover-note">
        {{ t(`subscriptionProgress.${subscriptionStore.loaded ? 'refreshError' : 'loadError'}`) }}
        <button type="button" class="subscription-text-button" :disabled="subscriptionStore.loading" @click="refresh">{{ t('subscriptionProgress.retry') }}</button>
      </p>
      <p v-else-if="!subscriptionStore.loaded" role="status" class="subscription-popover-note">{{ t('subscriptionProgress.loading') }}</p>
      <div v-for="plan in plans" :key="plan.id" class="subscription-plan" :data-plan-id="plan.id">
        <h4>{{ planName(plan) }}</h4>
        <p class="subscription-expiry">{{ expiryLabel(plan.expiresAt) }}</p>
        <dl class="subscription-windows">
          <div v-for="window in plan.windows" :key="window.period" class="subscription-window">
            <dt>{{ periodLabel(window) }}</dt>
            <dd>
              <strong>{{ t('subscriptionProgress.remaining') }} {{ remainingLabel(window) }}</strong>
              <span>{{ t('subscriptionProgress.used') }} {{ window.used === null ? t('subscriptionProgress.unknown') : money(window.used) }} / {{ t('subscriptionProgress.limit') }} {{ limitLabel(window) }}</span>
            </dd>
          </div>
        </dl>
      </div>
      <router-link to="/subscriptions" class="subscription-view-all" @click="closeTooltip()">{{ t('subscriptionProgress.viewAll') }}</router-link>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSubscriptionStore } from '@/stores'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { formatSubscriptionMoney as money, getExpirationDateRelation } from '@/utils/subscriptionQuota'
import type { SubscriptionAllowance, SubscriptionQuotaWindow } from '@/utils/subscriptionQuota'

const { t } = useI18n()
const subscriptionStore = useSubscriptionStore()
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const tooltipOpen = ref(false)
const popoverTop = ref(72)
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))
const plans = computed(() => subscriptionStore.allowances)
const selected = computed(() => plans.value[0])
const visible = computed(() => plans.value.length > 0 || !subscriptionStore.loaded || Boolean(subscriptionStore.error))
const triggerLabel = computed(() => {
  if (!selected.value) return t('subscriptionProgress.viewDetails')
  const plan = selected.value
  const allowance = plan.state === 'finite' && plan.limitingWindow
    ? `${periodLabel(plan.limitingWindow)} ${t('subscriptionProgress.remaining')} ${money(plan.limitingWindow.remaining!)}`
    : t(`subscriptionProgress.${plan.state === 'unlimited' ? 'unlimited' : 'unknown'}`)
  return `${planName(plan)}: ${allowance}. ${t('subscriptionProgress.viewDetails')}`
})

function planName(plan: SubscriptionAllowance) {
  return plan.name || t('subscriptionProgress.planFallback', { id: plan.groupId })
}
function periodLabel(window: SubscriptionQuotaWindow) {
  return t(`subscriptionProgress.${window.oneTime ? 'oneTime' : window.period}`)
}
function remainingLabel(window: SubscriptionQuotaWindow) {
  return window.state === 'finite' ? money(window.remaining!) : t(`subscriptionProgress.${window.state}`)
}
function limitLabel(window: SubscriptionQuotaWindow) {
  return window.state === 'finite' ? money(window.limit!) : t(`subscriptionProgress.${window.state}`)
}
function expiryLabel(expiresAt: string | null) {
  if (!expiresAt) return t('subscriptionProgress.noExpiry')
  const now = new Date(subscriptionStore.currentTime)
  const relation = getExpirationDateRelation(expiresAt, now)
  if (!relation) return t('subscriptionProgress.unknown')
  if (relation === 'expired') return t('subscriptionProgress.expired')
  if (relation === 'today') return t('subscriptionProgress.expiresToday')
  if (relation === 'tomorrow') return t('subscriptionProgress.expiresTomorrow')
  return t('subscriptionProgress.daysRemaining', { days: Math.ceil((Date.parse(expiresAt) - now.getTime()) / 86_400_000) })
}
function refresh() {
  if (subscriptionFeatureEnabled.value) subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
}
function toggleTooltip() {
  popoverTop.value = (triggerRef.value?.getBoundingClientRect().bottom || 64) + 8
  tooltipOpen.value = !tooltipOpen.value
}
function closeTooltip(restoreFocus = false) {
  tooltipOpen.value = false
  if (restoreFocus) triggerRef.value?.focus()
}
function handleClickOutside(event: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) closeTooltip()
}
function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && tooltipOpen.value) {
    event.preventDefault()
    closeTooltip(true)
  }
}
function handleFocusOut(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && !containerRef.value?.contains(event.relatedTarget)) closeTooltip()
}
watch(subscriptionFeatureEnabled, (enabled) => {
  if (!enabled) closeTooltip()
  else subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeydown)
  if (subscriptionFeatureEnabled.value) subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.subscription-mini { min-width: 0; max-width: 22rem; color: var(--ag-ink, #000); }
.subscription-mini-trigger { display: flex; min-width: 0; max-width: 100%; flex-direction: column; align-items: flex-start; padding: 0.35rem 0.5rem; border: 1px solid var(--ag-rule, currentColor); background: var(--ag-paper, #fff); text-align: left; line-height: 1.3; }
.subscription-mini-name { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.75rem; }
.subscription-mini-value { font-size: 0.75rem; font-weight: 600; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.subscription-mini-more { margin-inline-start: 0.4rem; font-weight: 400; }
.subscription-popover { position: fixed; top: var(--subscription-popover-top); right: 1rem; z-index: 60; width: min(30rem, calc(100vw - 2rem)); max-height: calc(100dvh - var(--subscription-popover-top) - 1rem); overflow-y: auto; border: 1px solid var(--ag-rule, currentColor); background: var(--ag-paper, #fff); padding: 1rem; color: var(--ag-ink, #000); }
.subscription-popover-heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; font-size: 0.875rem; font-weight: 600; }
.subscription-close { min-width: 2rem; min-height: 2rem; font-size: 1.25rem; }
.subscription-popover-note, .subscription-expiry { margin-top: 0.4rem; font-size: 0.75rem; line-height: 1.5; }
.subscription-plan { margin-top: 1rem; padding-top: 1rem; border-top: 1px solid var(--ag-rule, currentColor); }
.subscription-plan h4 { font-size: 0.875rem; font-weight: 600; overflow-wrap: anywhere; }
.subscription-windows { margin-top: 0.75rem; display: grid; gap: 0.65rem; font-size: 0.75rem; }
.subscription-window { display: grid; grid-template-columns: minmax(4rem, 1fr) minmax(0, 3fr); gap: 0.5rem; }
.subscription-window dd { display: flex; flex-direction: column; gap: 0.2rem; text-align: right; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.subscription-window strong { font-weight: 600; }
.subscription-text-button, .subscription-view-all { text-decoration: underline; text-underline-offset: 3px; }
.subscription-view-all { display: block; margin-top: 1rem; padding-top: 0.75rem; border-top: 1px solid var(--ag-rule, currentColor); font-size: 0.75rem; }
button:focus-visible, a:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
@media (max-width: 639px) {
  .subscription-mini { max-width: min(45vw, 14rem); }
  .subscription-mini-trigger { padding: 0.25rem 0.35rem; }
  .subscription-mini-name { font-size: 0.625rem; }
  .subscription-mini-value { font-size: 0.6875rem; }
}
</style>
