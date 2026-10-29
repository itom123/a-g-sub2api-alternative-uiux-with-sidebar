<template>
  <AppLayout>
    <div class="ag-page connections-page">
      <div class="ag-page-heading">
        <div>
          <p class="eyebrow">AG_&gt; · 链接</p>
          <h1>{{ t('connections.dashboardTitle') }}</h1>
          <p>{{ t('connections.dashboardDescription') }}</p>
        </div>
        <button v-if="!instance && lifecycle.can_create" class="btn btn-primary" :disabled="creating" @click="openCreateModal">
          {{ t('connections.createInstance') }}
        </button>
        <button v-else-if="!instance && !lifecycle.self_service_enabled && !lifecycle.create_request_pending" class="btn btn-primary" @click="requestLifecycleApproval('create')">
          {{ t('connections.requestCreate') }}
        </button>
      </div>

      <p v-if="error" class="ag-error" role="alert">{{ error }}</p>
      <div v-if="loading" class="loading-state" aria-live="polite">
        <span class="loading-spinner" aria-hidden="true"></span>
        <div><strong>{{ t('connections.loading') }}</strong><p>{{ t('connections.loadingDescription') }}</p></div>
      </div>

      <div v-else-if="!instance && lifecycle.recreate_allowed_at" class="buffer-panel">
        <div class="empty-mark">07</div>
        <h2>{{ t('connections.cooldownTitle') }}</h2>
        <p>{{ t('connections.cooldownDescription') }}</p>
        <p class="buffer-date">{{ t('connections.cooldownUntil', { date: formatDateTime(lifecycle.recreate_allowed_at) }) }}</p>
      </div>

      <div v-else-if="!instance" class="ag-empty">
        <div class="empty-mark">01</div>
        <h2>{{ t('connections.emptyTitle') }}</h2>
        <p>{{ t('connections.emptyDescription') }}</p>
        <button v-if="lifecycle.can_create" class="btn btn-primary" @click="openCreateModal">{{ t('connections.createFirst') }}</button>
        <button v-else-if="!lifecycle.self_service_enabled && !lifecycle.create_request_pending" class="btn btn-primary" @click="requestLifecycleApproval('create')">{{ t('connections.requestCreate') }}</button>
        <p v-if="!lifecycle.self_service_enabled" class="setup-note">{{ lifecycle.create_request_pending ? t('connections.createRequestPending') : t('connections.approvalRequired') }}</p>
      </div>

      <template v-else>
        <section class="tenant-hero">
          <div>
            <p class="section-kicker">{{ t('connections.tenantSpace', { hub: lifecycle.hub_name || 'Claw Hub I' }) }}</p>
            <div class="tenant-name-row">
              <h2>{{ instance.name }}</h2>
              <span :class="['state-pill', `state-${deletionQueued ? instance.status : (dashboard?.fleet_state || instance.status)}`]">{{ instanceStatus(deletionQueued ? instance.status : (dashboard?.fleet_state || instance.status)) }}</span>
            </div>
            <p class="instance-meta">{{ t('connections.privateWorkspace') }} · {{ formatDate(instance.created_at) }}</p>
          </div>
          <div class="tenant-actions">
            <button v-if="!deletionQueued" class="btn btn-secondary btn-sm" @click="openRenameModal">{{ t('connections.rename') }}</button>
            <button v-if="!deletionQueued && needsProvision" class="btn btn-primary btn-sm" :disabled="provisioning" @click="provision">
              {{ provisioning ? t('connections.provisioning') : t('connections.provisionAction') }}
            </button>
            <button v-if="dashboard?.gateway_enabled && !deletionQueued && (instance.status === 'ready' || instance.status === 'running')" class="btn btn-primary btn-sm" :disabled="openingGateway" @click="openGateway">
              {{ openingGateway ? t('connections.gatewayOpening') : t('connections.openGateway') }}
            </button>
            <button v-if="!deletionQueued && instance.status === 'stopped'" class="btn btn-secondary btn-sm" @click="run(() => startOpenClawInstance(workspaceId))">{{ t('connections.start') }}</button>
            <button v-else-if="!deletionQueued && (instance.status === 'ready' || instance.status === 'running')" class="btn btn-secondary btn-sm" @click="run(() => stopOpenClawInstance(workspaceId))">{{ t('connections.stop') }}</button>
            <button v-if="deletionQueued" class="btn btn-secondary btn-sm" disabled>{{ t('connections.deleteQueuedTitle') }}</button>
            <button v-else-if="lifecycle.can_delete" class="btn btn-danger btn-sm" @click="remove(instance)">{{ t('connections.delete') }}</button>
            <button v-else-if="!lifecycle.delete_request_pending" class="btn btn-secondary btn-sm" @click="requestLifecycleApproval('delete')">{{ t('connections.requestDelete') }}</button>
            <button v-else class="btn btn-secondary btn-sm" disabled>{{ t('connections.deleteRequestPending') }}</button>
          </div>
        </section>

        <p v-if="!lifecycle.self_service_enabled" class="approval-banner">
          {{ lifecycle.delete_request_pending ? t('connections.deleteRequestPending') : t('connections.approvalRequired') }}
        </p>

        <section v-if="deletionQueued" class="buffer-panel buffer-panel-compact" aria-live="polite">
          <p class="section-kicker">{{ t('connections.deletionBufferLabel') }}</p>
          <h2>{{ t('connections.deleteQueuedTitle') }}</h2>
          <p>{{ t('connections.deleteQueuedDescription') }}</p>
          <p v-if="instance.deletion_scheduled_at" class="buffer-date">{{ t('connections.deleteScheduledFor', { date: formatDateTime(instance.deletion_scheduled_at) }) }}</p>
        </section>

        <section v-if="needsProvision || provisioning" class="provision-panel" aria-live="polite">
          <div class="provision-copy">
            <p class="section-kicker">{{ t('connections.tenantSpace', { hub: lifecycle.hub_name || 'Claw Hub I' }) }}</p>
            <h2>{{ provisioning ? t('connections.provisioningTitle') : t('connections.notProvisionedTitle') }}</h2>
            <p>{{ provisioning ? t('connections.provisioningDescription') : t('connections.notProvisionedDescription') }}</p>
          </div>
          <div class="provision-progress" aria-hidden="true"><span :class="{ active: provisioning }"></span></div>
          <p class="provision-eta">{{ provisioning ? t('connections.provisioningEta') : t('connections.retryProvisioning') }}</p>
        </section>

        <section class="metric-grid" aria-label="Tenant health">
          <article class="metric-card"><span>{{ t('connections.metrics.latency') }}</span><strong>{{ dashboard?.latency_ms != null ? `${dashboard.latency_ms} ms` : '—' }}</strong><small>{{ t('connections.metrics.latencyHint') }}</small></article>
          <article class="metric-card"><span>{{ t('connections.metrics.connections') }}</span><strong>{{ dashboard?.connected_count || 0 }}/{{ dashboard?.connection_count || 0 }}</strong><small>{{ t('connections.metrics.connectionsHint') }}</small></article>
          <article class="metric-card"><span>{{ t('connections.metrics.activity') }}</span><strong><time v-if="lastConnectionActivity" :datetime="lastConnectionActivity" :aria-label="new Date(lastConnectionActivity).toLocaleString()" :title="new Date(lastConnectionActivity).toLocaleString()">{{ formatActivityTime(lastConnectionActivity) }}</time><template v-else>{{ t('connections.metrics.noActivity') }}</template></strong><small>{{ t('connections.metrics.activityHint') }}</small></article>
          <article class="metric-card"><span>{{ t('connections.metrics.checked') }}</span><strong>{{ dashboard?.checked_at ? formatTime(dashboard.checked_at) : '—' }}</strong><small>{{ t('connections.metrics.checkedHint') }}</small></article>
        </section>

        <section class="billing-api-panel" aria-labelledby="openclaw-api-title">
          <div>
            <p class="section-kicker">{{ t('connections.billingApiKicker') }}</p>
            <h2 id="openclaw-api-title">{{ t('connections.billingApiTitle') }}</h2>
            <p>{{ t('connections.billingApiDescription') }}</p>
          </div>
          <div v-if="dashboard?.api_key" class="billing-api-stats">
            <div class="billing-api-name"><span>{{ t('connections.billingApiName') }}</span><strong>{{ dashboard.api_key.name }}</strong></div>
            <div><span>{{ t('connections.billingApiStatus') }}</span><strong>{{ apiKeyStatus(dashboard.api_key.status) }}</strong></div>
            <div><span>{{ t('connections.billingApiUsage') }}</span><strong>{{ dashboard.api_key.usage_count }}</strong></div>
            <div><span>{{ t('connections.billingApiSpent') }}</span><strong>${{ dashboard.api_key.usage_cost.toFixed(4) }}</strong></div>
            <router-link class="btn btn-secondary btn-sm" to="/keys">{{ t('connections.billingApiOpenKeys') }}</router-link>
            <router-link class="btn btn-primary btn-sm" to="/purchase">{{ t('connections.billingApiTopUp') }}</router-link>
          </div>
          <p v-else class="billing-api-missing">{{ t('connections.billingApiMissing') }}</p>
        </section>

        <GatewayDevices v-if="dashboard?.gateway_enabled && !deletionQueued && ['ready', 'running'].includes(fleetState)" :key="`${workspaceId}-${gatewayDevicesRevision}`" :instance-id="workspaceId" />
        <GatewayEnrollment v-if="dashboard?.enrollment_enabled && !deletionQueued && ['ready', 'running'].includes(fleetState)" :key="`enroll-${workspaceId}`" :instance-id="workspaceId" @changed="gatewayDevicesRevision++" />

        <section class="panel-section">
          <div class="section-heading">
            <div><p class="section-kicker">{{ t('connections.accountsHeading') }}</p><h2>{{ t('connections.accountsTitle') }}</h2></div>
            <span class="catalog-note">{{ t('connections.catalogNote') }}</span>
          </div>
          <div class="connection-grid">
            <article v-for="kind in connectionKinds" :key="kind.kind" class="connection-card">
              <div class="provider-heading">
                <div class="provider-glyph">{{ providerMark(kind.kind) }}</div>
                <div><h3>{{ kind.title }}</h3><p>{{ kind.description }}</p></div>
              </div>
              <div class="connection-footer">
                <span :class="['status-pill', connectionFor(kind.kind) ? `connection-${connectionFor(kind.kind)?.status}` : 'connection-empty']">{{ connectionStatus(connectionFor(kind.kind)?.status) }}</span>
                <button class="btn btn-secondary btn-sm" type="button" @click="openConnection(kind)">{{ connectionFor(kind.kind) ? t('connections.manage') : setupAvailable(kind) ? t('connections.setUp') : t('connections.setupUnavailable') }}</button>
              </div>
            </article>
          </div>
        </section>
      </template>
    </div>

    <BaseDialog :show="showCreateModal" :title="t('connections.createTitle')" width="narrow" @close="closeCreateModal">
      <form class="dialog-form" @submit.prevent="createInstance">
        <p class="dialog-intro">{{ t('connections.createDescription') }}</p>
        <p class="setup-note">{{ t('connections.deletionWarning') }}</p>
        <label class="field-label" for="workspace-name">{{ t('connections.workspaceName') }}</label>
        <input id="workspace-name" v-model="workspaceName" class="field-input" maxlength="100" :placeholder="t('connections.workspacePlaceholder')" autocomplete="off" />
        <p v-if="createError" class="field-error">{{ createError }}</p>
        <div v-if="creating" class="inline-progress" aria-live="polite">
          <span class="loading-spinner" aria-hidden="true"></span>
          <div><strong>{{ t('connections.creating') }}</strong><p>{{ t('connections.creatingDescription') }}</p></div>
        </div>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" :disabled="creating" @click="closeCreateModal">{{ t('connections.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="creating || !workspaceName.trim()">{{ creating ? t('connections.creating') : t('connections.createWorkspace') }}</button>
        </div>
      </form>
    </BaseDialog>

    <BaseDialog :show="showRenameModal" :title="t('connections.renameTitle')" width="narrow" @close="closeRenameModal">
      <form class="dialog-form" @submit.prevent="renameInstance">
        <p class="dialog-intro">{{ t('connections.renameDescription') }}</p>
        <label class="field-label" for="rename-workspace">{{ t('connections.workspaceName') }}</label>
        <input id="rename-workspace" v-model="renameName" class="field-input" maxlength="100" autocomplete="off" />
        <p v-if="renameError" class="field-error">{{ renameError }}</p>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" :disabled="renaming" @click="closeRenameModal">{{ t('connections.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="renaming || !renameName.trim()">{{ renaming ? t('connections.renaming') : t('connections.saveName') }}</button>
        </div>
      </form>
    </BaseDialog>

    <BaseDialog v-if="activeKind" :show="showConnectionModal" :title="activeKind.title" width="normal" @close="showConnectionModal = false">
      <div class="dialog-form">
        <p class="dialog-kicker">{{ instance?.name }}</p>
        <h3 class="dialog-heading">{{ t('connections.setupTitle', { provider: activeKind.title }) }}</h3>
        <p class="dialog-intro">{{ t('connections.setupDescription', { provider: activeKind.title }) }}</p>
        <div class="provider-instructions">
          <template v-if="normalizedKind(activeKind.kind) === 'telegram'">
            <p>{{ t('connections.telegramSetup') }}</p>
            <p v-if="telegramAvailable" class="mt-2" data-testid="site-bot-hint">{{ t('connections.siteBotHint') }}</p>
          </template>
          <p v-else-if="normalizedKind(activeKind.kind) === 'gws'">{{ t('connections.gwsSetup') }}</p>
          <p v-else-if="normalizedKind(activeKind.kind) === 'wechat'">{{ t('connections.wechatSetup') }}</p>
          <p v-else>{{ t('connections.genericSetup') }}</p>
        </div>
        <div v-if="setupAvailable(activeKind)" class="telegram-form">
          <template v-if="normalizedKind(activeKind.kind) === 'telegram' && (!activeConnection || activeConnection.status !== 'connected')">
            <label class="field-label" for="telegram-token">{{ t('connections.telegramToken') }}</label>
            <input id="telegram-token" v-model="telegramToken" class="field-input" type="password" autocomplete="off" :placeholder="t('connections.telegramTokenPlaceholder')" />
          </template>
          <template v-else-if="normalizedKind(activeKind.kind) === 'wechat'">
            <div v-if="activeConnection?.setup_url" class="wechat-login-box">
              <a class="btn btn-primary" :href="activeConnection.setup_url" target="_blank" rel="noopener">{{ t('connections.wechatOpenLogin') }}</a>
              <p class="setup-note">{{ t('connections.wechatScanHint') }}</p>
              <p v-if="activeConnection.setup_message" class="setup-note">{{ activeConnection.setup_message }}</p>
            </div>
            <p v-if="activeConnection?.status === 'connected'" class="setup-success">{{ t('connections.wechatConnected') }}</p>
          </template>
          <template v-else-if="normalizedKind(activeKind.kind) === 'gws'">
            <div v-if="activeConnection?.setup_url" class="wechat-login-box">
              <a class="btn btn-primary" :href="activeConnection.setup_url" target="_blank" rel="noopener">{{ t('connections.gwsOpenLogin') }}</a>
              <p class="setup-note">{{ t('connections.gwsCodeHint') }}</p>
              <p v-if="activeConnection.setup_message" class="setup-note">{{ activeConnection.setup_message }}</p>
            </div>
            <label v-if="activeConnection?.setup_key" class="field-label" for="google-code">{{ t('connections.gwsCode') }}</label>
            <input v-if="activeConnection?.setup_key" id="google-code" v-model="googleCode" class="field-input" type="text" autocomplete="off" :placeholder="t('connections.gwsCodePlaceholder')" />
            <p v-if="activeConnection?.status === 'connected'" class="setup-success">{{ t('connections.gwsConnected') }}</p>
          </template>
          <p v-if="setupPrepared && normalizedKind(activeKind.kind) === 'telegram'" class="setup-success">{{ t('connections.setupPrepared') }}</p>
        </div>
        <p v-else class="setup-note">{{ t('connections.providerUnavailable', { provider: activeKind.title }) }}</p>
        <div class="dialog-actions">
          <button type="button" class="btn btn-secondary" @click="showConnectionModal = false">{{ t('connections.close') }}</button>
          <button v-if="setupAvailable(activeKind) && (!activeConnection || activeConnection.status !== 'connected')" type="button" class="btn btn-primary" :disabled="preparingConnection || (normalizedKind(activeKind.kind) === 'telegram' && !telegramToken.trim()) || (normalizedKind(activeKind.kind) === 'gws' && !!activeConnection?.setup_key && !googleCode.trim())" @click="prepareConnection">
            {{ preparingConnection ? (normalizedKind(activeKind.kind) === 'wechat' ? t('connections.wechatWaiting') : t('connections.connecting')) : normalizedKind(activeKind.kind) === 'wechat' && activeConnection ? t('connections.checkWeChatLogin') : normalizedKind(activeKind.kind) === 'gws' && activeConnection?.setup_key ? t('connections.gwsComplete') : normalizedKind(activeKind.kind) === 'gws' ? t('connections.gwsStart') : normalizedKind(activeKind.kind) === 'wechat' ? t('connections.setUp') : t('connections.connectTelegram') }}
          </button>
          <button v-if="activeConnection" type="button" class="btn btn-danger" :disabled="deletingConnection" @click="removeConnection(activeConnection)">
            {{ deletingConnection ? t('connections.removing') : t('connections.removeConnection') }}
          </button>
        </div>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  deleteOpenClawConnection,
  deleteOpenClawInstance,
  createOpenClawConnection,
  completeOpenClawConnection,
  createOpenClawAccessRequest,
  createOpenClawInstance,
  getOpenClawDashboard,
  getOpenClawGatewayLaunch,
  getOpenClawLifecycle,
  listOpenClawConnectionKinds,
  listOpenClawConnections,
  provisionOpenClawInstance,
  renameOpenClawInstance,
  startOpenClawInstance,
  stopOpenClawInstance,
  type OpenClawConnection,
  type OpenClawConnectionKindInfo,
  type OpenClawDashboard,
  type OpenClawInstance,
  type OpenClawLifecycle,
} from '@/api/openclaw'
import { useTelegramAvailability } from '@/composables/useTelegramAvailability'
import GatewayDevices from '@/components/openclaw/GatewayDevices.vue'
import GatewayEnrollment from '@/components/openclaw/GatewayEnrollment.vue'

const { t } = useI18n()
const { telegramAvailable } = useTelegramAvailability()
const instance = ref<OpenClawInstance | null>(null)
const lifecycle = ref<OpenClawLifecycle>({ can_create: false })
const dashboard = ref<OpenClawDashboard | null>(null)
const gatewayDevicesRevision = ref(0)
const connections = ref<OpenClawConnection[]>([])
const connectionKinds = ref<OpenClawConnectionKindInfo[]>([])
const loading = ref(true)
const creating = ref(false)
const provisioning = ref(false)
const openingGateway = ref(false)
const renaming = ref(false)
const preparingConnection = ref(false)
const deletingConnection = ref(false)
const error = ref('')
const createError = ref('')
const renameError = ref('')
const workspaceName = ref('')
const renameName = ref('')
const showCreateModal = ref(false)
const showRenameModal = ref(false)
const showConnectionModal = ref(false)
const activeKind = ref<OpenClawConnectionKindInfo | null>(null)
const telegramToken = ref('')
const googleCode = ref('')
const setupPrepared = ref(false)
const workspaceId = computed(() => instance.value?.id || 0)
const fleetState = computed(() => dashboard.value?.fleet_state || instance.value?.status || 'unknown')
const needsProvision = computed(() => ['planned', 'missing', 'error', 'unavailable'].includes(fleetState.value))
const deletionQueued = computed(() => Boolean(instance.value?.deletion_scheduled_at) || instance.value?.status === 'deletion_scheduled')
const activeConnection = computed(() => activeKind.value ? connectionFor(activeKind.value.kind) : undefined)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [nextLifecycle, kinds] = await Promise.all([getOpenClawLifecycle(), listOpenClawConnectionKinds()])
    lifecycle.value = nextLifecycle
    instance.value = nextLifecycle.instance || null
    connectionKinds.value = kinds
    dashboard.value = null
    connections.value = []
    if (instance.value) {
      const [nextConnections, nextDashboard] = await Promise.all([
        listOpenClawConnections(instance.value.id),
        getOpenClawDashboard(),
      ])
      connections.value = nextConnections
      dashboard.value = nextDashboard
    }
  } catch (err: any) {
    error.value = err?.message || t('connections.loadFailed')
  } finally {
    loading.value = false
  }
}

function openCreateModal() { if (!lifecycle.value.can_create) return; createError.value = ''; workspaceName.value = ''; showCreateModal.value = true }
function closeCreateModal() { if (!creating.value) showCreateModal.value = false }
async function createInstance() {
  if (!workspaceName.value.trim() || !lifecycle.value.can_create) return
  creating.value = true
  createError.value = ''
  try { await createOpenClawInstance(workspaceName.value.trim()); showCreateModal.value = false; await load() }
  catch (err: any) { createError.value = err?.message || t('connections.createFailed') }
  finally { creating.value = false }
}
async function requestLifecycleApproval(action: 'create' | 'delete') {
  const instanceId = action === 'delete' ? instance.value?.id : undefined
  const reason = window.prompt(t(action === 'create' ? 'connections.createRequestPrompt' : 'connections.deleteRequestPrompt'), '')
  if (reason === null) return
  error.value = ''
  try {
    await createOpenClawAccessRequest(action, instanceId, reason)
    await load()
  } catch (err: any) { error.value = err?.message || t('connections.requestFailed') }
}
function openRenameModal() { if (!instance.value) return; renameName.value = instance.value.name; renameError.value = ''; showRenameModal.value = true }
function closeRenameModal() { if (!renaming.value) showRenameModal.value = false }
async function renameInstance() {
  if (!instance.value || !renameName.value.trim()) return
  renaming.value = true
  renameError.value = ''
  try { instance.value = await renameOpenClawInstance(instance.value.id, renameName.value.trim()); showRenameModal.value = false }
  catch (err: any) { renameError.value = err?.message || t('connections.renameFailed') }
  finally { renaming.value = false }
}
async function provision() {
  if (!instance.value) return
  provisioning.value = true
  error.value = ''
  try { instance.value = await provisionOpenClawInstance(instance.value.id); await load() }
  catch (err: any) { error.value = err?.message || t('connections.provisionFailed') }
  finally { provisioning.value = false }
}
async function openGateway() {
  if (!instance.value || openingGateway.value) return
  const popup = window.open('about:blank', '_blank')
  if (!popup) { error.value = t('connections.gatewayPopupBlocked'); return }
  popup.opener = null
  openingGateway.value = true
  error.value = ''
  try {
    const result = await getOpenClawGatewayLaunch(instance.value.id)
    const destination = new URL(result.url)
    if (destination.protocol !== 'https:' || destination.username || destination.password) throw new Error('Invalid gateway URL')
    popup.location.replace(destination.href)
  } catch { popup.close(); error.value = t('connections.gatewayOpenFailed') }
  finally { openingGateway.value = false }
}
async function run(action: () => Promise<void>) {
  error.value = ''
  try { await action(); await load() } catch (err: any) { error.value = err?.message || t('connections.actionFailed') }
}
async function remove(target: OpenClawInstance) {
  if (!window.confirm(t('connections.deleteConfirm', { name: target.name }))) return
  await run(async () => { await deleteOpenClawInstance(target.id) })
}
function openConnection(kind: OpenClawConnectionKindInfo) { activeKind.value = kind; telegramToken.value = ''; googleCode.value = ''; setupPrepared.value = false; showConnectionModal.value = true }
function normalizedKind(kind: string) { return kind.trim().toLowerCase() }
function connectionFor(kind: string) {
  const normalized = normalizedKind(kind)
  return connections.value.find(connection => normalizedKind(connection.kind) === normalized)
}
function setupAvailable(kind: OpenClawConnectionKindInfo) {
  return kind.setup_available ?? normalizedKind(kind.kind) === 'telegram'
}
async function prepareConnection() {
  if (!instance.value || !activeKind.value || !setupAvailable(activeKind.value)) return
  preparingConnection.value = true
  error.value = ''
  try {
    const kind = normalizedKind(activeKind.value.kind)
    const connection = activeConnection.value?.setup_key && (kind === 'wechat' || kind === 'gws')
      ? await completeOpenClawConnection(instance.value.id, activeConnection.value.id, kind === 'gws' ? googleCode.value.trim() : activeConnection.value.setup_key)
      : await createOpenClawConnection(instance.value.id, activeKind.value.kind, '', kind === 'telegram' ? telegramToken.value.trim() : '')
    const index = connections.value.findIndex(item => item.id === connection.id)
    if (index >= 0) connections.value.splice(index, 1, connection)
    else connections.value = [...connections.value, connection]
    telegramToken.value = ''
    googleCode.value = ''
    setupPrepared.value = connection.status === 'connected'
  } catch (err: any) { error.value = err?.message || t('connections.connectFailed') }
  finally { preparingConnection.value = false }
}
async function removeConnection(connection: OpenClawConnection) {
  if (!instance.value) return
  deletingConnection.value = true
  try { await deleteOpenClawConnection(instance.value.id, connection.id); connections.value = connections.value.filter(item => item.id !== connection.id); showConnectionModal.value = false }
  catch (err: any) { error.value = err?.message || t('connections.connectFailed') }
  finally { deletingConnection.value = false }
}
function connectionStatus(status?: string) { return status ? t(`connections.status.${status}`, status) : t('connections.status.notConnected') }
function instanceStatus(status: string) { return t(`connections.instanceStatus.${status}`, status) }
function apiKeyStatus(status: string) { return t(`connections.apiKeyStatus.${status}`, status) }
function providerMark(kind: string) { const normalized = normalizedKind(kind); return normalized === 'wechat' ? '微' : normalized === 'telegram' ? 'TG' : normalized === 'gws' ? 'G' : normalized.slice(0, 2).toUpperCase() }
function formatDate(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value)) }
function formatDateTime(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
const lastConnectionActivity = computed(() => {
  const value = dashboard.value?.last_used_at
  return value && Number.isFinite(Date.parse(value)) ? value : null
})
function formatActivityTime(value: string) {
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZoneName: 'short' }).format(new Date(value))
}
function formatTime(value: string) { return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }

onMounted(load)
</script>

<style scoped>
.connections-page { width: 100%; max-width: none; padding: 4px 0 48px; }
.connections-page .ag-page-heading > div:first-child { min-width: 0; }
.loading-state { display: flex; align-items: center; gap: 14px; min-height: 116px; border: 1px solid var(--ag-ink); padding: 24px; }
.loading-state p { margin: 5px 0 0; font-size: 13px; line-height: 1.6; opacity: .7; }
.inline-progress { display: flex; align-items: center; gap: 12px; border: 1px solid var(--ag-ink); padding: 12px; }
.inline-progress p { margin: 4px 0 0; font-size: 12px; line-height: 1.5; opacity: .7; }
.loading-spinner { width: 24px; height: 24px; flex: 0 0 24px; border: 2px solid var(--ag-ink); border-right-color: transparent !important; border-radius: 50% !important; animation: connections-spin .8s linear infinite; }
.provision-panel { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(180px, .7fr); align-items: center; gap: 24px; border: 1px solid var(--ag-ink); padding: 22px 24px; }
.provision-copy h2 { margin: 0; font-size: 22px; }
.provision-copy p:last-child { max-width: 68ch; margin: 8px 0 0; font-size: 14px; line-height: 1.6; opacity: .75; }
.provision-progress { height: 8px; overflow: hidden; border: 1px solid var(--ag-ink); }
.provision-progress span { display: block; width: 28%; height: 100%; background: var(--ag-ink); }
.provision-progress span.active { animation: connections-progress 1.4s ease-in-out infinite; }
.provision-eta { grid-column: 2; margin: -12px 0 0; font-size: 12px; line-height: 1.5; opacity: .7; }
.buffer-panel { margin-top: 28px; border: 1px solid #a33c2d; padding: 24px; }
.buffer-panel h2 { margin: 0; font-size: 22px; }
.buffer-panel p:not(.section-kicker):not(.buffer-date) { max-width: 70ch; margin: 8px 0 0; font-size: 14px; line-height: 1.6; }
.buffer-panel-compact { margin-top: 20px; }
.buffer-date { margin: 14px 0 0; font-weight: 600; }
.eyebrow { margin: 0 0 8px; font-size: 12px; letter-spacing: .14em; text-transform: uppercase; opacity: .7; }
.tenant-hero { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 18px; margin-top: 28px; padding: 24px; border: 1px solid var(--ag-ink); }
.section-kicker { margin: 0 0 8px; font-size: 11px; letter-spacing: .14em; text-transform: uppercase; opacity: .65; }
.tenant-name-row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.tenant-hero h2 { margin: 0; font-size: 28px; }
.instance-meta { margin: 6px 0 0; font-size: 12px; text-transform: uppercase; letter-spacing: .08em; opacity: .65; }
.tenant-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.metric-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 1px; margin-top: 20px; background: var(--ag-ink); border: 1px solid var(--ag-ink); }
.metric-card { min-width: 0; overflow-wrap: anywhere; display: flex; min-height: 120px; flex-direction: column; justify-content: space-between; padding: 16px; background: var(--ag-paper, #f4f1e8); }
.metric-card span, .metric-card small { font-size: 11px; text-transform: uppercase; letter-spacing: .08em; opacity: .65; }
.metric-card strong { font-size: 25px; font-weight: 500; }
.billing-api-panel { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 20px; margin-top: 20px; border: 1px solid var(--ag-ink); padding: 20px 24px; }
.billing-api-panel h2 { margin: 0; font-size: 22px; }
.billing-api-panel p:not(.section-kicker):not(.billing-api-missing) { max-width: 62ch; margin: 7px 0 0; font-size: 13px; line-height: 1.55; opacity: .72; }
.billing-api-stats { display: flex; flex-wrap: wrap; align-items: end; justify-content: flex-end; gap: 18px; }
.billing-api-stats div { display: flex; min-width: 82px; flex-direction: column; gap: 5px; }
.billing-api-stats span { font-size: 10px; text-transform: uppercase; letter-spacing: .08em; opacity: .6; }
.billing-api-stats strong { font-size: 17px; font-weight: 500; }
.billing-api-missing { margin: 0; color: #a33c2d; font-size: 13px; }
.panel-section { margin-top: 30px; }
.section-heading { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.section-heading h2 { margin: 0; font-size: 22px; }
.catalog-note { font-size: 12px; opacity: .65; }
.connection-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 1px; background: var(--ag-ink); border: 1px solid var(--ag-ink); }
.connection-card { display: flex; min-height: 190px; flex-direction: column; justify-content: space-between; gap: 18px; padding: 20px; background: var(--ag-paper, #f4f1e8); }
.provider-heading { display: flex; gap: 12px; }
.provider-glyph { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border: 1px solid currentColor; color: #28704d; font-size: 12px; font-weight: 700; }
.connection-card h3 { margin: 0; font-size: 17px; }
.connection-card p { margin: 8px 0 0; font-size: 13px; line-height: 1.55; opacity: .75; }
.connection-footer { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.state-pill, .status-pill { display: inline-flex; align-items: center; min-height: 22px; padding: 3px 8px; border: 1px solid currentColor; font-size: 10px; text-transform: uppercase; letter-spacing: .08em; }
.state-ready, .connection-ready { color: #28704d; }
.state-provisioning, .state-planned, .state-missing, .state-deletion_scheduled, .connection-pending { color: #9a6a22; }
.state-error { color: #a33c2d; }
.state-unavailable { color: #a33c2d; }
.connection-empty { opacity: .6; }
.ag-error, .field-error { color: #a33c2d; }
.empty-mark { margin-bottom: 16px; color: #a33c2d; font: 600 12px/1 ui-monospace, SFMono-Regular, Menlo, monospace; }
.dialog-form { display: flex; flex-direction: column; gap: 12px; }
.dialog-intro { margin: 0; font-size: 14px; line-height: 1.6; opacity: .75; }
.dialog-kicker { margin: 0; font-size: 11px; letter-spacing: .12em; text-transform: uppercase; opacity: .6; }
.dialog-heading { margin: 0; font-size: 21px; }
.field-label { font-size: 13px; font-weight: 600; }
.field-input { width: 100%; border: 1px solid var(--ag-ink); background: transparent; padding: 11px 12px; color: inherit; }
.field-input:focus { outline: 2px solid #a33c2d; outline-offset: 1px; }
.dialog-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.setup-note { margin: 4px 0 0; padding: 12px; border-left: 3px solid #a33c2d; font-size: 12px; line-height: 1.55; background: color-mix(in srgb, #a33c2d 7%, transparent); }
.provider-instructions { border: 1px solid var(--ag-ink); padding: 14px; font-size: 13px; line-height: 1.65; }
.provider-instructions p { margin: 0; }
.setup-success { margin: 0; border-left: 3px solid #28704d; padding: 10px 12px; font-size: 13px; line-height: 1.5; }
@keyframes connections-spin { to { transform: rotate(360deg); } }
@keyframes connections-progress { 0% { transform: translateX(-120%); } 100% { transform: translateX(380%); } }
@media (max-width: 840px) { .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .provision-panel { grid-template-columns: 1fr; } .provision-eta { grid-column: auto; margin: -10px 0 0; } }
@media (max-width: 560px) { .metric-grid { grid-template-columns: 1fr; } .tenant-hero { align-items: flex-start; padding: 18px; } .tenant-actions { width: 100%; } .tenant-actions .btn { flex: 1 1 140px; } .connection-footer { align-items: flex-start; flex-direction: column; } .connection-footer .btn { width: 100%; } .provision-panel { padding: 18px; } .billing-api-panel { align-items: flex-start; padding: 18px; } .billing-api-stats { width: 100%; justify-content: flex-start; } }
@media (prefers-reduced-motion: reduce) { .loading-spinner, .provision-progress span.active { animation: none; } }
</style>
