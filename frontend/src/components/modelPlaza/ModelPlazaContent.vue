<template>
  <div class="ag-page space-y-5">
    <div class="ag-page-heading"><div><h1>模型广场</h1><p>先选任务分组，再复制准确的模型 ID。余额与额度由账户共享。</p></div><button class="btn btn-secondary" :disabled="live.loading" @click="live.refresh(true)">{{ live.loading ? '更新中…' : '刷新目录与状态' }}</button></div>
    <StatusFreshness v-if="isAuthenticated && live.monitorEnabled" />
    <p v-if="error && response" role="alert">目录更新失败，正在显示上次成功的数据（{{ live.catalogUpdatedAt }}）。请刷新重试。</p>
    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-gray-100 bg-white px-5 py-4 text-sm shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
      v-html="descriptionHtml"
    ></div>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="flex items-center gap-1.5 text-xs text-gray-400 dark:text-dark-500"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error && !response"
      class="rounded-2xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
      <button class="btn btn-secondary ml-3" @click="live.refresh(true)">重试</button>
    </div>
    <template v-else>
      <!-- 筛选区:平台 → 分组 → 倍率 -->
      <PlazaFilterBar
        v-if="groupOptions.length"
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
      />

      <label v-if="groupOptions.length" class="ag-toolbar text-sm">模型近期观测
        <select v-model="selectedHealth" class="input !w-auto"><option value="all">全部状态</option><option value="healthy">正常</option><option value="warning">性能下降</option><option value="critical">异常</option><option value="unknown">待观测</option></select>
      </label>
      <!-- 分组分节的模型清单(默认按生效倍率升序) -->
      <div v-if="filteredGroups.length > 0" class="space-y-8">
        <nav aria-label="跳转到分组" class="flex flex-wrap gap-x-5 gap-y-2 border-y py-3 text-sm">
          <a v-for="group in filteredGroups" :key="group.id" :href="`#plaza-group-${group.id}`" class="underline underline-offset-4">{{ groupDisplayName(group.name, group.platform) }}</a>
        </nav>
        <p class="max-w-prose text-sm leading-6">Token 单价以人民币／百万 Token 展示，使用当前汇率换算。额度按整数元展示，实际请求按精确用量累计。分组白名单决定可用模型；目录收录与近期健康观测分别更新。</p>
        <section v-for="collection in groupCollections" :key="collection.key" class="space-y-4">
          <div class="border-b pb-3"><h2 class="text-lg font-semibold">{{ collection.title }}</h2><p class="mt-1 text-sm">{{ collection.description }}</p></div>
          <PlazaGroupSection v-for="g in collection.groups" :id="`plaza-group-${g.id}`" :key="g.id" :group="g" class="scroll-mt-6" />
        </section>
      </div>
      <div
        v-else
        class="rounded-2xl border border-dashed border-gray-300 px-5 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      >
        <template v-if="authStore.isAdmin && !searchActive">{{ t('modelPlaza.adminEmpty') }}<div class="ag-toolbar mt-4 justify-center"><RouterLink to="/admin/channels/pricing" class="btn btn-secondary">{{ t('modelPlaza.manageTeamPricing') }}</RouterLink><RouterLink to="/available-channels" class="btn btn-secondary">{{ t('modelPlaza.viewConfiguredGroups') }}</RouterLink></div></template>
        <template v-else>{{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}</template>
      </div>
    </template>
    <div class="ag-links"><RouterLink to="/available-channels">查看可用分组</RouterLink><RouterLink to="/monitor">查看完整状态</RouterLink></div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { groupDisplayName } from '@/utils/groupDisplayName'
import { useRoute } from 'vue-router'
import { useModelStatusStore } from '@/stores/modelStatus'
import StatusFreshness from '@/components/goblin/StatusFreshness.vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const live = useModelStatusStore()
const selectedHealth = ref('all')
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const route = useRoute()
const selectedGroupId = ref<number | 'all'>(Number(route.query.group) || 'all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')

watch([selectedGroupId, selectedPlatform], ([group, platform]) => {
  live.filter.groupIds = group === 'all' ? [] : [group]
  live.filter.platforms = platform === 'all' ? [] : [platform]
}, { immediate: true })

const searchActive = computed(() => searchQuery.value.trim() !== '' || selectedPlatform.value !== 'all' || selectedGroupId.value !== 'all' || selectedRate.value !== 'all' || selectedHealth.value !== 'all')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const platforms = computed(() =>
  [...new Set((props.response?.groups ?? []).map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  (props.response?.groups ?? []).map((g) => ({
    id: g.id,
    name: groupDisplayName(g.name, g.platform),
    platform: g.platform,
    rate: effectiveRate(g)
  }))
)

/** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
const rates = computed(() =>
  [...new Set((props.response?.groups ?? []).map(effectiveRate))].sort((a, b) => a - b)
)

/** 数据刷新后选中的倍率可能不复存在,重置为全部。 */
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

function taskOrder(g: ModelPlazaGroup) {
  const index = ['通用', '编程', '推理'].indexOf(groupDisplayName(g.name, g.platform))
  return index < 0 ? 3 : index
}

const filteredGroups = computed(() => {
  let groups = (props.response?.groups ?? []).map(g => ({ ...g, models: g.models.filter(m => m.name !== '__other__') }))
  if (selectedHealth.value !== 'all') groups = groups.map(g => ({ ...g, models: g.models.filter(m => (live.findModel(m.platform, m.name)?.health.overall || 'unknown') === selectedHealth.value) })).filter(g => g.models.length > 0)
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => effectiveRate(g) === selectedRate.value)
  }
  // 模型名搜索:分组内只留命中的模型,整组无命中则隐藏该分组。
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => ({ ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }))
      .filter((g) => g.models.length > 0)
  }
  // 专属倍率会改变生效值,不能只依赖后端按默认倍率的排序。
  return [...groups].sort(
    (a, b) => effectiveRate(a) - effectiveRate(b) || taskOrder(a) - taskOrder(b) || a.name.localeCompare(b.name)
  )
})

const groupCollections = computed(() => [
  { key: 'standard', title: '按用量使用', description: '根据任务选择模型和推理强度；使用账户余额与个人平台额度。', groups: filteredGroups.value.filter(g => g.subscription_type !== 'subscription' && !g.is_exclusive) },
  { key: 'subscription', title: '团队订阅分组', description: '同一账户的密钥共享该账户订阅额度，不同账户分别计量；请使用本人获授权的分组。', groups: filteredGroups.value.filter(g => g.subscription_type === 'subscription') },
  { key: 'exclusive', title: '专属分组', description: '仅向获得授权的账户展示，价格与规则以各组配置为准。', groups: filteredGroups.value.filter(g => g.subscription_type !== 'subscription' && g.is_exclusive) },
].filter(collection => collection.groups.length > 0))
</script>

<style scoped>
.plaza-description :deep(*) { color: var(--ag-ink) !important; border-color: var(--ag-ink) !important; background-color: var(--ag-paper) !important; }
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
