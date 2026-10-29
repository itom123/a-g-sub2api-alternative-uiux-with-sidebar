<template>
  <BaseDialog :show="!!apiKey" :title="t('keys.workBuddySetup.title')" width="wide" @close="$emit('close')">
    <div class="space-y-5">
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('keys.workBuddySetup.steps') }}</p>
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('keys.workBuddySetup.group', { name: apiKey?.group?.name || '—' }) }}</p>
      <p v-if="loading" role="status" class="text-sm">{{ t('keys.workBuddySetup.loading') }}</p>
      <p v-if="discoveryFallback" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ t('keys.workBuddySetup.catalogFallback') }}</p>
      <p v-if="discoveryError" role="alert" class="text-sm text-amber-700 dark:text-amber-400">{{ t('keys.workBuddySetup.discoveryError') }}</p>
      <label class="block text-sm font-medium">
        {{ t('keys.workBuddySetup.model') }}
        <select v-if="models.length" v-model="model" class="input mt-1 w-full">
          <option v-for="id in models" :key="id" :value="id">{{ id }}</option>
        </select>
        <input v-else v-model="model" :disabled="loading" class="input mt-1 w-full" />
      </label>
      <p class="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm font-medium text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200">{{ t('keys.workBuddySetup.exactId') }}</p>
      <div class="space-y-2 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
        <div v-for="field in connectionFields" :key="field.label" class="flex items-center justify-between gap-3">
          <div class="min-w-0 text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ field.label }}</span>
            <code class="block break-all">{{ field.display }}</code>
          </div>
          <button class="btn btn-secondary shrink-0" :disabled="!field.value" :aria-label="`${t('keys.workBuddySetup.copy')} ${field.label}`" @click="copyToClipboard(field.value)">{{ t('keys.workBuddySetup.copy') }}</button>
        </div>
      </div>
      <div>
        <h4 class="mb-2 font-semibold">{{ t('keys.workBuddySetup.advanced') }}</h4>
        <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">{{ t('keys.workBuddySetup.catalogHint') }}</p>
        <p class="mb-2 text-sm font-medium">{{ t('keys.workBuddySetup.groupCeiling', { limit: effortLabels[apiKey?.group?.max_reasoning_effort || ''] || t('keys.workBuddySetup.unlimited') }) }}</p>
        <p v-if="apiKey?.group?.max_reasoning_effort" class="mb-2 text-sm">{{ t(apiKey.group.max_reasoning_effort_over_limit === 'deny' ? 'keys.workBuddySetup.ceilingDeny' : 'keys.workBuddySetup.ceilingDowngrade') }}</p>
        <ul v-if="capabilities.changes.length" class="mb-3 space-y-1 text-sm">
          <li v-for="change in capabilities.changes" :key="change.from">{{ effortLabels[change.from] || change.from }} → {{ change.to === 'deny' ? t('keys.workBuddySetup.denied') : effortLabels[change.to] || change.to }}</li>
        </ul>
        <dl class="divide-y divide-gray-100 rounded-xl border border-gray-200 px-3 dark:divide-dark-700 dark:border-dark-600">
          <div v-for="setting in settings" :key="setting.label" class="flex justify-between gap-4 py-2 text-sm">
            <dt>{{ setting.label }}</dt>
            <dd class="max-w-[60%] text-right font-medium">{{ setting.value }}</dd>
          </div>
        </dl>
      </div>
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('keys.workBuddySetup.finish') }}</p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useClipboard } from '@/composables/useClipboard'
import { buildCodexModelCatalogUrl, fetchCodexModelsManifest } from '@/api/codex'
import { getWorkBuddyCapabilities, parseWorkBuddyModelIds, workBuddyEndpoint, workBuddyEffortLabels, type WorkBuddyModelMetadata } from '@/utils/workbuddySetup'
import { getModelPlaza } from '@/api/modelPlaza'
import type { ApiKey } from '@/types'

const props = defineProps<{ apiKey: ApiKey | null; baseUrl: string }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const model = ref('')
const models = ref<string[]>([])
const metadata = ref<WorkBuddyModelMetadata[]>([])
const loading = ref(false)
const discoveryError = ref(false)
const discoveryFallback = ref(false)
const capabilities = computed(() => getWorkBuddyCapabilities(metadata.value.find(entry => entry.slug === model.value), props.apiKey?.group, model.value))
const connectionFields = computed(() => [
  { label: t('keys.workBuddySetup.url'), value: workBuddyEndpoint(props.baseUrl), display: workBuddyEndpoint(props.baseUrl) },
  { label: t('keys.workBuddySetup.model'), value: model.value, display: model.value || '—' },
  { label: t('keys.apiKey'), value: props.apiKey?.key || '', display: '••••••••' }
])
const effortLabels = workBuddyEffortLabels
const status = (value: boolean | undefined) => t(`keys.workBuddySetup.${value === undefined ? 'unknown' : value ? 'on' : 'off'}`)
const settings = computed(() => {
  const cap = capabilities.value
  return [
    { label: t('keys.workBuddySetup.toolCalling'), value: status(cap.toolCalling) },
    { label: t('keys.workBuddySetup.imageInput'), value: status(cap.imageInput) },
    { label: t('keys.workBuddySetup.reasoning'), value: status(cap.reasoning) },
    { label: t('keys.workBuddySetup.reasoningOnly'), value: status(cap.reasoningOnly) },
    { label: t('keys.workBuddySetup.allowDisablingReasoning'), value: status(cap.allowDisablingReasoning) },
    { label: t('keys.workBuddySetup.customProtocol'), value: t('keys.workBuddySetup.standardProtocol') },
    { label: t('keys.workBuddySetup.defaultEffort'), value: cap.defaultEffort ? effortLabels[cap.defaultEffort] : t(cap.nativeEfforts.length && !cap.efforts.length ? 'keys.workBuddySetup.noRecommendedEfforts' : 'keys.workBuddySetup.auto') },
    { label: t('keys.workBuddySetup.supportedEfforts'), value: cap.efforts.length ? cap.efforts.map(effort => effortLabels[effort]).join(', ') : t(cap.nativeEfforts.length ? 'keys.workBuddySetup.noRecommendedEfforts' : 'keys.workBuddySetup.unknown') },
    { label: t('keys.workBuddySetup.input'), value: t('keys.workBuddySetup.providerDefault') },
    { label: t('keys.workBuddySetup.output'), value: t('keys.workBuddySetup.providerDefault') }
  ]
})
watch(() => props.apiKey, async (key, _previous, onCleanup) => {
  model.value = ''
  models.value = []
  metadata.value = []
  discoveryError.value = false
  discoveryFallback.value = false
  loading.value = !!key
  if (!key) return
  let active = true
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 15000)
  const discoveryController = new AbortController()
  const discoveryTimeout = setTimeout(() => discoveryController.abort(), 8000)
  controller.signal.addEventListener('abort', () => discoveryController.abort(), { once: true })
  onCleanup(() => { active = false; clearTimeout(timeout); clearTimeout(discoveryTimeout); controller.abort() })
  // Discovery stays on the portal origin to avoid cross-origin gateway restrictions.
  // The configured public base URL is still used for the WorkBuddy connection form.
  const gatewayBase = window.location.origin
  const catalog = fetchCodexModelsManifest(gatewayBase, key.key, controller.signal)
    .then(result => JSON.parse(result.content).models as WorkBuddyModelMetadata[]).catch(() => [])
  try {
    let ids: string[] = []
    try {
      const response = await fetch(buildCodexModelCatalogUrl(gatewayBase), {
        headers: { Authorization: `Bearer ${key.key}` }, cache: 'no-store', signal: discoveryController.signal
      })
      if (!response.ok) throw new Error('Model discovery failed')
      ids = parseWorkBuddyModelIds(await response.json())
      if (!ids.length) throw new Error('Empty model list')
    } catch {
      clearTimeout(discoveryTimeout)
      const plaza = await getModelPlaza({ signal: controller.signal })
      const group = plaza.groups.find(group => group.id === (key.group_id ?? key.group?.id))
      ids = [...new Set(group?.models.map(entry => entry.name).filter(name => name && name !== '__other__') || [])]
      if (active) discoveryFallback.value = ids.length > 0
    }
    if (!active) return
    models.value = ids
    model.value = ids[0] || ''
    discoveryError.value = !ids.length
    const entries = await catalog
    if (active) metadata.value = entries
  } catch {
    if (active) discoveryError.value = true
  } finally {
    clearTimeout(timeout)
    clearTimeout(discoveryTimeout)
    if (active) { loading.value = false; if (!models.value.length) discoveryError.value = true }
  }
})
</script>
