<template>
  <AppLayout>
    <div class="ag-page">
      <div class="ag-page-heading"><div><h1>{{ auth.isAdmin ? '已配置分组' : '可用分组' }}</h1><p>{{ auth.isAdmin ? '检查团队渠道和三个任务组的真实配置。' : '按任务找到自己的路由分组、支持模型与适用倍率。账户余额在各组间共享。' }}</p></div><button class="btn btn-secondary" :disabled="auth.isAdmin ? configured?.loading : live.loading" @click="refresh">{{ (auth.isAdmin ? configured?.loading : live.loading) ? '更新中…' : '刷新分组与状态' }}</button></div>
      <StatusFreshness v-if="live.monitorEnabled && !auth.isAdmin" />
      <label class="ag-toolbar">搜索任务、分组或模型<input v-model="searchQuery" type="search" class="input !max-w-md" placeholder="例如 通用、gpt-6-luna" /></label>
      <ConfiguredChannels v-if="auth.isAdmin" ref="configured" :search="searchQuery" />
      <template v-else>
      <p v-if="live.channelsError" role="alert">分组获取失败。{{ live.channelsUpdatedAt ? `保留上次数据（${live.channelsUpdatedAt}），请刷新重试。` : '请刷新重试或重新登录。' }}</p>
      <p v-if="live.loading && !live.channelsUpdatedAt">正在读取账户权限…</p>
      <div v-else-if="!live.channels.length && !live.channelsError" class="ag-empty">当前账户没有可用渠道或分组权限。请联系管理员确认授权；<RouterLink to="/profile">查看账户信息</RouterLink>。</div>
      <div v-else-if="!filtered.length && live.channels.length" class="ag-empty">没有符合搜索条件的分组。<button class="btn btn-secondary" @click="searchQuery = ''">清除搜索</button></div>
      <section v-for="(channel, index) in filtered" :key="`${channel.name}:${index}`" class="ag-channel">
        <header><h2>{{ channel.name }}</h2><p>{{ channel.description || '暂无任务说明' }}</p></header>
        <div v-for="section in channel.platforms" :key="section.platform" class="ag-channel-section">
          <span class="ag-status">{{ section.platform }}</span>
          <div class="ag-channel-groups"><div v-for="group in section.groups" :key="group.id"><strong>{{ groupDisplayName(group.name, group.platform) }}</strong><span>适用倍率 {{ live.rates[group.id] ?? group.rate_multiplier }}×{{ live.rates[group.id] != null ? '（个人倍率）' : '' }}</span><span v-if="group.peak_rate_enabled">高峰 {{ formatPeakRateWindow(group, serverTimezoneLabel(app.cachedPublicSettings?.server_utc_offset)) }} · {{ group.peak_rate_multiplier }}×</span><RouterLink :to="{ path: '/model-plaza', query: { group: group.id } }">查看本组完整计价</RouterLink></div></div>
          <p class="ag-note">模型单价可点击或聚焦名称查看；该单价尚未乘分组／个人倍率。实付、分时及长上下文规则以模型广场为准。</p>
          <div v-if="section.supported_models.length" class="ag-channel-models"><article v-for="model in section.supported_models.filter(m => m.name !== '__other__')" :key="`${model.platform}:${model.name}`"><SupportedModelChip :model="model" pricing-key-prefix="availableChannels.pricing" no-pricing-label="暂未提供定价" :show-platform="false" :platform-hint="section.platform" /><ModelHealth :platform="model.platform || section.platform" :model="model.name" /><RouterLink :to="{ path: '/monitor', query: { platform: model.platform || section.platform, model: model.name } }">查看此模型观测</RouterLink></article></div>
          <div v-else class="ag-empty">此渠道尚未配置可用模型。请联系管理员补充模型映射。</div>
        </div>
      </section>
      </template>
      <div class="ag-links"><RouterLink to="/model-plaza">模型广场</RouterLink><RouterLink to="/monitor">完整模型状态</RouterLink></div>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { groupDisplayName } from '@/utils/groupDisplayName'
import { computed, ref } from 'vue'
import ConfiguredChannels from '@/components/channels/ConfiguredChannels.vue'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import SupportedModelChip from '@/components/channels/SupportedModelChip.vue'
import ModelHealth from '@/components/goblin/ModelHealth.vue'
import StatusFreshness from '@/components/goblin/StatusFreshness.vue'
import { useLiveModelStatus } from '@/composables/useLiveModelStatus'
import { useAppStore } from '@/stores/app'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
const live = useLiveModelStatus()
const app = useAppStore()
const auth = useAuthStore()
const configured = ref<InstanceType<typeof ConfiguredChannels>>()
function refresh() {
  if (auth.isAdmin) configured.value?.refresh()
  else live.refresh(true)
}
const searchQuery = ref('')
live.filter = { range: '24h', platforms: [], groupIds: [], models: [] }
const filtered = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return live.channels.filter(ch => !q || [ch.name, ch.description, ...ch.platforms.flatMap(p => [p.platform, ...p.groups.map(g => g.name), ...p.supported_models.map(m => m.name)])].join(' ').toLowerCase().includes(q))
})
</script>
<style scoped>
.ag-channel { border: 1px solid var(--ag-ink); }
.ag-channel header { padding: 24px; border-bottom: 1px solid var(--ag-ink); }
h2 { font-size: 28px; font-weight: 700; }
header p { margin-top: 12px; line-height: 1.7; }
.ag-channel-section { padding: 24px; }
.ag-channel-section + .ag-channel-section { border-top: 1px solid var(--ag-ink); }
.ag-channel-groups { display: flex; flex-wrap: wrap; gap: 24px; margin: 16px 0; }
.ag-channel-groups > div { display: flex; flex-wrap: wrap; align-items: baseline; gap: 12px; font-size: 14px; }
.ag-channel a { text-decoration: underline; text-underline-offset: 4px; font-size: 12px; }
.ag-note { font-size: 12px; line-height: 1.7; }
.ag-channel-models { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr)); gap: 24px; margin-top: 24px; }
.ag-channel-models article { border-top: 1px solid var(--ag-ink); padding-top: 16px; min-width: 0; }
</style>
