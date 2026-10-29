<template>
  <div class="ag-landing">
    <header class="ag-landing-nav">
      <RouterLink class="ag-brand" to="/home">{{ app.siteName }}</RouterLink>
      <nav aria-label="主导航">
        <RouterLink v-if="settings?.model_plaza_enabled" :to="plazaTarget">模型广场</RouterLink>
        <RouterLink v-if="settings?.available_channels_enabled" to="/available-channels">可用分组</RouterLink>
        <RouterLink v-if="settings?.channel_monitor_enabled" to="/monitor">模型状态</RouterLink>
        <RouterLink :to="dashboard">{{ auth.isAuthenticated ? '我的账户' : '登录' }}</RouterLink>
        <CurrencyMenu />
        <ThemeToggle />
      </nav>
    </header>
    <main>
      <section class="ag-hero">
        <div>
          <h1>按任务选模型。<br>按状态发起请求。</h1>
          <p>算法哥布林把通用、编程与推理任务放进清晰的分组。查看账户可用模型、计量规则和近期状态，再开始工作。</p>
          <div class="ag-toolbar"><RouterLink v-if="settings?.model_plaza_enabled" :to="plazaTarget" class="btn btn-primary">进入模型广场</RouterLink><a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary">阅读使用文档</a></div>
        </div>
        <aside class="ag-access">
          <h2>一个入口，三类任务</h2><p>请按任务明确选择模型。各分组共同使用账户余额，请先查看自己的额度。</p>
          <dl><dt>OpenAI 兼容基础地址</dt><dd class="ag-model-id">{{ apiBase }}</dd><dt>个人凭证</dt><dd><RouterLink to="/keys">管理我的密钥</RouterLink></dd></dl>
          <p class="ag-key-note">首次登录请立即修改密码。密钥仅由本人保管，请勿在邮件、聊天或截图中暴露。</p>
        </aside>
      </section>
      <section v-if="auth.isAuthenticated" class="ag-home-live">
        <div class="ag-page-heading"><h2>当前可用数据</h2><button class="btn btn-secondary" :disabled="live.loading" @click="live.refresh(true)">{{ live.loading ? '更新中…' : '刷新' }}</button></div>
        <StatusFreshness v-if="live.monitorEnabled" />
        <label v-if="live.monitorEnabled" class="ag-observation-toggle"><input v-model="showUnobserved" type="checkbox" /> 显示待观测模型 <span v-if="hiddenModelCount">（已隐藏 {{ hiddenModelCount }} 个）</span></label>
        <p v-if="live.catalogError" role="alert">模型目录刷新失败。请重试；已有目录保留供参考。</p>
        <p v-if="live.channelsError" role="alert">可用分组刷新失败，请重试；已有分组保留供参考。</p>
        <div v-if="visibleGroups.length" class="ag-live-groups"><article v-for="group in visibleGroups" :key="group.id"><h3>{{ groupDisplayName(group.name, group.platform) }}</h3><div v-for="model in group.models" :key="`${model.platform}:${model.name}`"><span class="ag-model-id">{{ model.name }}</span><ModelHealth :platform="model.platform" :model="model.name" compact /></div></article></div>
        <p v-else-if="!live.loading && (live.catalogError || live.channelsError)">无法确认当前可用模型，请刷新重试。</p>
        <p v-else-if="!live.loading && !settings?.available_channels_enabled">可用分组未启用。</p>
        <p v-else-if="!live.loading && availableGroups.length">当前暂无近期观测。勾选“显示待观测模型”查看可用目录，或前往<RouterLink :to="plazaTarget">模型广场</RouterLink>。</p>
        <p v-else-if="!live.loading && auth.isAdmin">当前管理员个人账户未分配业务分组。<RouterLink to="/available-channels">查看团队已配置分组</RouterLink>，或使用业务账户查看自己的模型目录。</p>
        <p v-else-if="!live.loading">暂无可用模型，请联系管理员确认分组权限。</p>
      </section>
      <section class="ag-home-section">
        <div class="ag-section-heading"><h2>按任务选择</h2><RouterLink v-if="settings?.available_channels_enabled" to="/available-channels">查看我的分组</RouterLink></div>
        <div class="ag-task-grid"><article><h3>通用</h3><p>写作、翻译、摘要、资料整理与基础教案，从日常事务开始。</p></article><article><h3>编程</h3><p>代码修改、工具调用与调试，以及较复杂的专业任务。</p></article><article><h3>推理</h3><p>复杂数学、多步分析和需要严密推导的问题。</p></article></div>
        <p class="ag-note">实际可用模型与计价以登录后的目录为准。客户端须明确选择模型；复杂任务请切换到对应分组。</p>
      </section>
      <section v-if="settings?.channel_monitor_enabled" class="ag-home-section ag-status-entry"><h2>先确认近期状态</h2><p>状态来自实际请求。近期无流量或样本不足时显示“待观测”；它不代表模型正常，也不代表模型异常。</p><RouterLink to="/monitor" class="btn btn-primary">打开状态面板</RouterLink></section>
      <section class="ag-home-section"><div class="ag-section-heading"><h2>开始一次请求</h2></div><ol class="ag-steps"><li><h3>登录并修改密码</h3><p>记住自己的密码，在账户内管理个人密钥与额度。</p></li><li><h3>按任务选择模型</h3><p>复制准确的模型 ID，使用对应分组的密钥与价格规则。</p></li><li><h3>确认状态并核验结果</h3><p>查看近期观测，发起请求后检查返回结果与用量记录。</p></li></ol></section>
    </main>
    <footer>Algorithmic Goblin / 算法哥布林 <span>模型、权限、价格与状态以账户实时数据为准。</span></footer>
  </div>
</template>
<script setup lang="ts">
import ThemeToggle from '@/components/common/ThemeToggle.vue'
import CurrencyMenu from '@/components/common/CurrencyMenu.vue'
import { groupDisplayName } from '@/utils/groupDisplayName'
import { computed, ref } from 'vue'
import { hasMonitorObservation } from '@/features/channel-monitor-v2/monitorFormat'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLiveModelStatus } from '@/composables/useLiveModelStatus'
import { sanitizeUrl } from '@/utils/url'
import ModelHealth from './ModelHealth.vue'
import StatusFreshness from './StatusFreshness.vue'
const app = useAppStore()
const auth = useAuthStore()
const live = useLiveModelStatus()
const availableGroups = computed(() => (live.catalog?.groups || []).filter(g => g.models.length && live.channels.some(ch => ch.platforms.some(p => p.groups.some(r => r.id === g.id)))))
const showUnobserved = ref(false)
const visibleGroups = computed(() => availableGroups.value.map(group => ({
  ...group,
  models: group.models.filter(model => {
    const row = live.findModel(model.platform, model.name)
    return !live.monitorEnabled || showUnobserved.value || Boolean(row && hasMonitorObservation(row.metrics, row.health))
  })
})).filter(group => group.models.length))
const hiddenModelCount = computed(() => availableGroups.value.reduce((sum, group) => sum + group.models.length, 0) - visibleGroups.value.reduce((sum, group) => sum + group.models.length, 0))
const settings = computed(() => app.cachedPublicSettings)
const dashboard = computed(() => auth.isAuthenticated ? auth.isAdmin ? '/admin/dashboard' : '/dashboard' : '/login')
const plazaTarget = computed(() => !auth.isAuthenticated && settings.value?.model_plaza_require_auth ? '/login?redirect=/model-plaza' : '/model-plaza')
const docUrl = computed(() => sanitizeUrl(settings.value?.doc_url || ''))
const apiBase = `${window.location.origin}/v1`
// Home summaries always describe the whole permitted catalog, not a previous monitor filter.
live.filter = { range: '24h', platforms: [], groupIds: [], models: [] }
</script>
<style scoped>
.ag-landing { max-width: 1244px; margin: auto; padding: 0 32px; line-height: 1.7; }
.ag-landing-nav { position: sticky; top: 0; z-index: 30; display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 16px; padding: 24px 0; border-block: 1px solid var(--ag-ink); background: var(--ag-paper); }
.ag-brand { font-size: 24px; font-weight: 700; }
nav { display: flex; align-items: center; flex-wrap: wrap; gap: 24px; font-size: 14px; }
.ag-hero { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr); gap: 48px; padding: 64px 0; align-items: start; }
h1 { font-size: clamp(38px, 5.4vw, 72px); line-height: 1.1; font-weight: 700; letter-spacing: -.045em; margin-bottom: 24px; }
h2 { font-size: 30px; line-height: 1.3; font-weight: 700; }
h3 { font-size: 24px; font-weight: 700; margin-bottom: 12px; }
.ag-hero p { max-width: 48ch; margin: 0 0 24px; }
.ag-access { border: 1px solid var(--ag-ink); padding: 24px; }
.ag-access h2 { font-size: 24px; margin-bottom: 16px; }
.ag-access dl { border-top: 1px solid var(--ag-ink); }
.ag-access dt { font-size: 12px; margin-top: 16px; }
.ag-access dd { margin-bottom: 12px; }
.ag-access .ag-key-note { border-top: 1px solid var(--ag-ink); padding-top: 16px; margin: 0; font-size: 12px; }
.ag-home-section, .ag-home-live { padding: 32px 0 48px; border-top: 1px solid var(--ag-ink); }
.ag-section-heading { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: baseline; gap: 16px; margin-bottom: 24px; }
.ag-task-grid, .ag-live-groups, .ag-steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 32px; }
.ag-task-grid article + article { border-left: 1px solid var(--ag-ink); padding-left: 32px; }
.ag-observation-toggle { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 12px 0; font-size: 14px; }
.ag-live-groups article, .ag-live-groups article > div { min-width: 0; overflow-wrap: anywhere; }
.ag-live-groups article > div { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--ag-rule); }
.ag-note { margin-top: 24px; font-size: 12px; }
.ag-status-entry p { max-width: 66ch; margin: 16px 0 24px; }
.ag-steps { list-style: decimal inside; }
.ag-steps h3 { font-size: 22px; margin-top: 12px; }
footer { border-top: 1px solid var(--ag-ink); padding: 24px 0; display: flex; flex-wrap: wrap; justify-content: space-between; gap: 16px; font-size: 12px; }
a:hover { text-decoration: underline; }
@media (max-width: 800px) { .ag-hero { grid-template-columns: 1fr; gap: 32px; padding: 48px 0; } .ag-task-grid, .ag-live-groups, .ag-steps { grid-template-columns: 1fr; gap: 24px; } .ag-task-grid article + article { border-left: 0; border-top: 1px solid var(--ag-ink); padding: 24px 0 0; } }
@media (max-width: 480px) { .ag-landing { padding: 0 20px; } h1 { font-size: 34px; } nav { gap: 12px 20px; } .ag-access { padding: 20px; } .ag-hero { padding: 32px 0; } }
</style>
