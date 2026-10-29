<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, provide, ref, shallowReactive, useId, watch } from 'vue'
import type { CSSProperties, ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountUsageSummaryKey } from './usageSummary'
import type { UsageSummary } from './usageSummary'

defineProps<{ accountName: string }>()
const { t } = useI18n()
const panelId = useId()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const content = ref<HTMLElement | null>(null)
const open = ref(false)
const position = ref<CSSProperties>({ visibility: 'hidden' })
const summaries = shallowReactive(new Map<string, ComputedRef<UsageSummary>>())
provide(accountUsageSummaryKey, summaries)
const windows = computed(() => Array.from(summaries, ([id, summary]) => ({ id, ...summary.value })))
let resizeObserver: ResizeObserver | undefined

async function toggle() {
  if (open.value) return close()
  position.value = { visibility: 'hidden' }
  open.value = true
  await nextTick()
  updatePosition()
  content.value?.querySelector<HTMLButtonElement>('.ag-usage-popover-header button')?.focus({ preventScroll: true })
}

function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}

function updatePosition() {
  const anchor = root.value?.getBoundingClientRect()
  const panel = content.value?.getBoundingClientRect()
  if (!anchor || !panel) return
  const leftSpace = anchor.left - 24
  const rightSpace = innerWidth - anchor.right - 24
  if (leftSpace >= panel.width || rightSpace >= panel.width) {
    position.value = {
      left: `${leftSpace >= panel.width ? anchor.left - panel.width - 12 : anchor.right + 12}px`,
      top: `${Math.max(12, Math.min(anchor.top, innerHeight - Math.min(panel.height, innerHeight - 24) - 12))}px`,
      maxHeight: `${innerHeight - 24}px`,
      visibility: 'visible',
    }
    return
  }
  const belowSpace = Math.max(0, innerHeight - anchor.bottom - 24)
  const aboveSpace = Math.max(0, anchor.top - 24)
  const below = panel.height <= belowSpace || belowSpace >= aboveSpace
  position.value = {
    top: below ? `${anchor.bottom + 12}px` : 'auto',
    bottom: below ? 'auto' : `${innerHeight - anchor.top + 12}px`,
    left: `${Math.max(12, Math.min(anchor.left, innerWidth - panel.width - 12))}px`,
    maxHeight: `${below ? belowSpace : aboveSpace}px`,
    visibility: 'visible',
  }
}

function outsideClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Node) || root.value?.contains(target) || content.value?.contains(target)) return
  if (target instanceof Element && target.closest('.ag-help-tooltip, .modal-content')) return
  close()
}

function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && !document.querySelector('.modal-overlay')) close(true)
}

function scroll(event: Event) {
  if (event.target instanceof Node && content.value?.contains(event.target)) return
  const anchor = root.value?.getBoundingClientRect()
  if (anchor && (anchor.bottom < 0 || anchor.top > innerHeight || anchor.right < 0 || anchor.left > innerWidth)) close()
  else updatePosition()
}

function removeListeners() {
  resizeObserver?.disconnect()
  resizeObserver = undefined
  document.removeEventListener('click', outsideClick, true)
  document.removeEventListener('keydown', keydown)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', scroll, true)
}

watch(open, (value) => {
  if (!value) return removeListeners()
  if (content.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(updatePosition)
    resizeObserver.observe(content.value)
  }
  document.addEventListener('click', outsideClick, true)
  document.addEventListener('keydown', keydown)
  window.addEventListener('resize', updatePosition)
  window.addEventListener('scroll', scroll, true)
})
onBeforeUnmount(removeListeners)
</script>

<template>
  <div ref="root" class="ag-usage-preview">
    <button
      ref="trigger"
      type="button"
      class="ag-usage-details-trigger"
      :aria-expanded="open"
      :aria-controls="panelId"
      aria-haspopup="dialog"
      :aria-label="`${t(open ? 'admin.accounts.usageWindow.hideDetails' : 'admin.accounts.usageWindow.details')} · ${accountName}`"
      :title="t('admin.accounts.usageWindow.detailsHint')"
      @click="toggle"
    >
      {{ t(open ? 'admin.accounts.usageWindow.hideDetails' : 'admin.accounts.usageWindow.details') }}
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
        <path :d="open ? 'm6 15 6-6 6 6' : 'm6 9 6 6 6-6'" />
      </svg>
    </button>
    <div v-if="open" class="ag-usage-preview-content ag-usage-retained-summary">
      <div class="ag-account-usage">
        <div class="space-y-1">
          <div v-for="window in windows" :key="window.id" class="ag-usage-window">
            <div class="ag-usage-window-heading">
              <strong>{{ window.label }}</strong>
              <span>{{ window.caption }} {{ window.percent }}</span>
            </div>
            <div class="ag-usage-meter" role="progressbar" :aria-label="`${window.label} · ${window.caption}`" :aria-valuenow="window.utilization" aria-valuemin="0" aria-valuemax="100" :aria-valuetext="window.percent">
              <span :style="{ width: `${window.utilization}%` }"></span>
            </div>
          </div>
          <p v-if="!windows.length" class="ag-usage-summary-empty">{{ t('admin.accounts.usageWindow.detailsOpen') }}</p>
        </div>
      </div>
    </div>
    <Teleport to="body" :disabled="!open">
      <div
        ref="content"
        :id="panelId"
        :class="open ? 'ag-usage-popover' : 'ag-usage-preview-content'"
        :style="open ? position : undefined"
        :role="open ? 'dialog' : undefined"
        :aria-label="open ? `${t('admin.accounts.columns.usageWindows')} · ${accountName}` : undefined"
      >
        <div v-if="open" class="ag-usage-popover-header">
          <div class="ag-usage-popover-identity">
            <strong>{{ accountName }}</strong>
            <span>{{ t('admin.accounts.columns.usageWindows') }}</span>
          </div>
          <button type="button" :aria-label="t('common.close')" @click="close(true)">×</button>
        </div>
        <slot />
      </div>
    </Teleport>
  </div>
</template>
