<template>
  <section
    class="overflow-hidden rounded-2xl border bg-white shadow-card dark:bg-dark-800/50"
    :class="[platformBorderStrongClass(group.platform)]"
  >
    <!-- 分组头部:名称/平台/倍率徽章/专属/订阅徽章 + 描述 -->
    <header class="border-b border-gray-100 px-5 py-4 dark:border-dark-700/60">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-2xl font-bold">{{ groupDisplayName(group.name, group.platform) }}</h2>
        <RouterLink :to="{ path: '/monitor', query: { group: String(group.id), platform: group.platform } }" class="text-sm underline underline-offset-4">查看本组状态</RouterLink>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <GroupBadge
          :name="groupDisplayName(group.name, group.platform)"
          :platform="group.platform as GroupPlatform"
          :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
          :rate-multiplier="group.rate_multiplier"
          :user-rate-multiplier="group.user_rate_multiplier ?? null"
          :peak-rate-enabled="group.peak_rate_enabled"
          :peak-start="group.peak_start"
          :peak-end="group.peak_end"
          :peak-rate-multiplier="group.peak_rate_multiplier"
          always-show-rate
        />
        <span
          v-if="group.is_exclusive"
          class="inline-flex items-center gap-1 rounded-md bg-purple-50 px-2 py-0.5 text-xs font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-400"
        >
          <Icon name="shield" size="xs" class="h-3 w-3" />
          {{ t('modelPlaza.badges.exclusive') }}
        </span>
        <span
          v-if="group.subscription_type === 'subscription'"
          class="inline-flex items-center rounded-md bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-600 dark:bg-violet-900/20 dark:text-violet-400"
        >
          {{ t('modelPlaza.badges.subscription') }}
        </span>
      </div>
      <p v-if="group.description" class="mt-2 text-sm text-gray-500 dark:text-dark-400">
        {{ group.description }}
      </p>
      <dl class="mt-4 grid gap-3 border-t pt-3 text-sm sm:grid-cols-3">
        <div><dt class="text-xs">模型目录</dt><dd class="mt-1 font-medium">{{ group.models.length }} 个模型 · 以本组权限为准</dd></div>
        <div v-if="group.max_reasoning_effort !== undefined"><dt class="text-xs">推理强度</dt><dd class="mt-1 font-medium">{{ group.max_reasoning_effort || '不设分组上限' }}<span v-if="group.max_reasoning_effort"> · {{ group.reasoning_effort_over_limit === 'reject' ? '超出时拒绝' : '超出时自动降级' }}</span></dd></div>
        <div><dt class="text-xs">额度规则</dt><dd class="mt-1">{{ quotaSummary }}</dd></div>
        <div v-if="group.rpm_limit"><dt class="text-xs">分组 RPM 配置</dt><dd class="mt-1">{{ group.rpm_limit }} 次／分钟</dd></div>
      </dl>
      <p
        v-if="peakNote"
        class="mt-1.5 inline-flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400"
      >
        <Icon name="clock" size="xs" class="h-3 w-3" />
        {{ peakNote }}
      </p>
      <p
        v-if="longContextNote"
        class="mt-1.5 flex items-center gap-1 text-xs text-gray-500 dark:text-dark-400"
      >
        <Icon name="infoCircle" size="xs" class="h-3 w-3" />
        {{ longContextNote }}
      </p>
    </header>

    <p v-if="live.channelsUpdatedAt && !channelLinked" class="px-5 py-4 text-sm">{{ group.subscription_type === 'subscription' ? '团队订阅通过本组所属账户调度，目录按本组白名单及计费规则展示；近期调用情况请查看本组状态。' : '当前暂无此组的可用渠道记录。目录价格供参考，使用前请联系管理员确认接入。' }}</p>
    <!-- 模型价格表:整行(含 hover 底色/分区底色)顶到卡片边缘,左右留白由表格首列/末列的 padding 提供 -->
    <div>
      <PlazaModelPricingTable
        v-if="group.models.length > 0"
        :models="group.models"
        :group-id="group.id"
        :platform="group.platform"
        :rate-multiplier="group.rate_multiplier"
        :user-rate-multiplier="group.user_rate_multiplier ?? null"
        :image-rate-independent="group.image_rate_independent"
        :image-rate-multiplier="group.image_rate_multiplier"
        :video-rate-independent="group.video_rate_independent"
        :video-rate-multiplier="group.video_rate_multiplier"
        :peak-window="peakWindow"
        :peak-rate-multiplier="group.peak_rate_multiplier"
      />
      <p v-else class="px-5 py-4 text-center text-sm text-gray-400 dark:text-dark-500">
        {{ t('modelPlaza.detail.noModels') }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { groupDisplayName } from '@/utils/groupDisplayName'
import { useModelStatusStore } from '@/stores/modelStatus'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlazaModelPricingTable from './PlazaModelPricingTable.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBorderStrongClass } from '@/utils/platformColors'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { useAppStore } from '@/stores/app'
import { formatCny } from '@/utils/currency'

const props = defineProps<{
  group: ModelPlazaGroup
}>()

const { t } = useI18n()
const appStore = useAppStore()
const live = useModelStatusStore()
const channelLinked = computed(() => live.channels.some(ch => ch.platforms.some(section => section.groups.some(g => g.id === props.group.id))))
const quotaSummary = computed(() => {
  const group = props.group
  const limits = ([['日', group.daily_limit_usd], ['周', group.weekly_limit_usd], ['月', group.monthly_limit_usd]] as const)
    .filter(([, value]) => value != null && value > 0)
    .map(([label, value]) => `${label} ${formatCny(value, 0)}`)
  if (limits.length) return `${limits.join(' / ')}${group.subscription_type === 'subscription' ? ' · 同一账户订阅内共享' : ''}`
  return group.daily_limit_usd === undefined ? '以账户和订阅配置为准' : '分组未设置周期上限；仍受账户及平台额度限制'
})

/** 高峰窗口描述(含倍率与服务器时区标注);分组未启用高峰为空串。 */
const peakWindow = computed(() => {
  if (!hasPeakRate(props.group)) return ''
  return formatPeakRateWindow(
    props.group,
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
})

const peakNote = computed(() => {
  if (!peakWindow.value) return ''
  return t('modelPlaza.detail.peakNote', {
    window: peakWindow.value,
    multiplier: props.group.peak_rate_multiplier
  })
})

/**
 * 分组关闭了长上下文阶梯、但组内有模型官方带阶梯时提示:实付列只展示基础档,
 * 官方阶梯仅供参考。字段缺失(旧后端)不提示。
 */
const longContextNote = computed(() => {
  if (props.group.long_context_pricing_enabled !== false) return ''
  const hasOfficialLadder = props.group.models.some(
    (m) => (m.official_pricing?.intervals?.length ?? 0) > 1
  )
  return hasOfficialLadder ? t('modelPlaza.detail.longContextDisabledNote') : ''
})
</script>
