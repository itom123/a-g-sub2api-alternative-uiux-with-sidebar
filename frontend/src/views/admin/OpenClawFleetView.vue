<template>
  <AppLayout>
    <div class="fleet-page space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.18em] text-primary-600">AG_&gt; · OpenClaw</p>
          <h1 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">Tenant fleet</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">Monitor every customer workspace on the Claw Hub I bridge.</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="load">Refresh</button>
      </div>

      <p v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>

      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <article v-for="metric in metrics" :key="metric.label" class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ metric.hint }}</p>
        </article>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="border-b border-gray-200 px-4 py-4 dark:border-dark-700">
          <h2 class="font-semibold text-gray-900 dark:text-white">Customer workspaces</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">View openai-龙虾 pricing, key binding, models, connected services, usage, and bridge health. Set group pricing and models in Group Management; workspaces pick up model changes automatically.</p>
          <div class="mt-3 flex flex-wrap items-center gap-3">
            <input v-model="search" class="input w-full max-w-md" placeholder="Search user or workspace" />
            <div class="w-full sm:w-auto sm:min-w-[180px]"><Select v-model="sortKey" :options="sortOptions" aria-label="Sort workspaces" /></div>
          </div>
        </div>
        <div v-if="loading" class="p-8 text-center text-sm text-gray-500">Loading fleet…</div>
        <div v-else-if="!filteredItems.length" class="p-8 text-center text-sm text-gray-500">No matching customer workspaces.</div>
        <div v-else class="overflow-x-auto">
          <table class="fleet-table min-w-full divide-y divide-gray-200 text-left text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
              <tr><th class="px-4 py-3">Workspace</th><th class="px-4 py-3">User</th><th class="px-4 py-3">State</th><th class="px-4 py-3">Billing</th><th class="px-4 py-3">Models</th><th class="px-4 py-3">Services</th><th class="px-4 py-3">Usage</th><th class="px-4 py-3">Deletion</th><th class="px-4 py-3 text-right">Action</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in filteredItems" :key="item.id" class="text-gray-700 dark:text-gray-200">
                <td data-label="Workspace" class="whitespace-nowrap px-4 py-4"><div class="font-medium">{{ item.name }}</div><div class="text-xs text-gray-400">{{ item.tenant_slug }}</div></td>
                <td data-label="User" class="whitespace-nowrap px-4 py-4">{{ item.user_email }}</td>
                <td data-label="State" class="whitespace-nowrap px-4 py-4"><span class="rounded-full border px-2 py-1 text-xs">{{ item.status }}</span></td>
                <td data-label="Billing" class="whitespace-nowrap px-4 py-4"><div class="font-medium">{{ item.billing_group || 'Unbound' }}</div><div class="text-xs text-gray-500">x{{ item.billing_rate.toFixed(2) }} · {{ item.api_key_status || 'no key' }}</div></td>
                <td data-label="Models" class="max-w-sm px-4 py-4">
                  <ul class="fleet-models text-xs"><li v-for="model in item.allowed_models" :key="model">{{ model }}</li><li v-if="!item.allowed_models.length">No allowed models</li></ul>
                  <div class="mt-1 text-xs text-gray-500">Default: {{ item.default_model || '—' }}</div>
                  <div class="text-xs text-gray-500">{{ modelsSource(item) }}</div>
                  <div v-if="item.mapping_error" class="mt-1 text-xs text-red-700 dark:text-red-300">Model mapping error: {{ item.mapping_error }}</div>
                  <div v-else-if="!item.mapping_enabled" class="text-xs text-gray-500">Not sent to the workspace: model mapping is off</div>
                  <div v-else-if="item.model_sync" :class="['fleet-sync mt-1 text-xs', modelSyncClass(item.model_sync)]" :title="item.model_synced_at ? formatDate(item.model_synced_at) : undefined">{{ modelSyncLabel(item.model_sync) }}</div>
                </td>
                <td data-label="Services" class="px-4 py-4">
                  <ul v-if="connectionsOf(item).length" class="fleet-services text-xs">
                    <li v-for="connection in connectionsOf(item)" :key="`${connection.kind}-${connection.label}`">
                      <span class="font-medium">{{ connection.title || connection.kind }}</span><span v-if="connection.label" class="text-gray-500"> · {{ connection.label }}</span>
                      <span :class="['fleet-service-state', connection.status === 'connected' ? 'text-green-700 dark:text-green-300' : 'text-amber-700 dark:text-amber-300']">{{ connectionStatus(connection.status) }}</span>
                    </li>
                  </ul>
                  <div v-else class="text-xs text-gray-500">No services</div>
                </td>
                <td data-label="Usage" class="whitespace-nowrap px-4 py-4">
                  <div v-if="item.request_count" class="fleet-usage">
                    <div><strong class="fleet-usage-cost">{{ money(item.usage_cost) }}</strong> <span class="text-xs text-gray-500">billed</span></div>
                    <div>{{ plural(item.request_count, 'request') }} · {{ formatCompactNumber(item.token_count) }} tokens</div>
                    <div class="text-xs text-gray-500">Last 7 days: {{ plural(item.requests_7d, 'request') }} · {{ money(item.usage_cost_7d) }}</div>
                    <div v-if="item.last_request_at" class="text-xs text-gray-500">Last call <time :datetime="item.last_request_at" :title="formatDate(item.last_request_at)">{{ formatRelativeTime(item.last_request_at) }}</time></div>
                  </div>
                  <div v-else class="text-xs text-gray-500">No API calls yet</div>
                </td>
                <td data-label="Deletion" class="whitespace-nowrap px-4 py-4 text-xs text-gray-500">{{ item.deletion_scheduled_at ? `Queued for ${formatDate(item.deletion_scheduled_at)}` : '—' }}</td>
                <td data-label="Action" class="whitespace-nowrap px-4 py-4 text-right"><div class="flex justify-end gap-2"><button class="btn btn-secondary btn-sm" :disabled="reconcileBusy === item.id" @click="reconcile(item)">{{ reconcileBusy === item.id ? 'Repairing…' : 'Repair OpenClaw binding' }}</button><button class="btn btn-secondary btn-sm" :disabled="policyBusy === item.user_id" @click="setSelfService(item.user_id, false)">{{ policyBusy === item.user_id ? 'Saving…' : 'Disable self-service' }}</button><button v-if="!item.deletion_scheduled_at" class="btn btn-danger btn-sm" @click="remove(item)">Queue delete</button><span v-else class="text-xs text-gray-500">Queued</span></div></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="border-b border-gray-200 px-4 py-4 dark:border-dark-700">
          <h2 class="font-semibold text-gray-900 dark:text-white">Access requests</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">Approve lifecycle access for accounts whose self-service switch is disabled.</p>
        </div>
        <div v-if="!requests.length" class="p-8 text-center text-sm text-gray-500">No pending access requests.</div>
        <div v-else class="overflow-x-auto">
          <table class="fleet-table min-w-full divide-y divide-gray-200 text-left text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
              <tr><th class="px-4 py-3">User</th><th class="px-4 py-3">Action</th><th class="px-4 py-3">Reason</th><th class="px-4 py-3">Created</th><th class="px-4 py-3 text-right">Decision</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="request in requests" :key="request.id" class="text-gray-700 dark:text-gray-200">
                <td data-label="User" class="whitespace-nowrap px-4 py-4"><div>{{ request.user_email || `User #${request.user_id}` }}</div><button class="mt-1 text-xs text-primary-600 hover:underline" :disabled="policyBusy === request.user_id" @click="setSelfService(request.user_id, true)">{{ policyBusy === request.user_id ? 'Saving…' : 'Enable self-service' }}</button></td>
                <td data-label="Action" class="whitespace-nowrap px-4 py-4">{{ request.action }}</td>
                <td data-label="Reason" class="max-w-md px-4 py-4 text-sm">{{ request.reason || '—' }}</td>
                <td data-label="Created" class="whitespace-nowrap px-4 py-4 text-xs text-gray-500">{{ formatDate(request.created_at) }}</td>
                <td data-label="Decision" class="whitespace-nowrap px-4 py-4 text-right"><div class="flex justify-end gap-2"><button class="btn btn-primary btn-sm" :disabled="decisionBusy === request.id" @click="decide(request, true)">Approve</button><button class="btn btn-danger btn-sm" :disabled="decisionBusy === request.id" @click="decide(request, false)">Reject</button></div></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import { cnyCost } from '@/utils/currency'
import { formatCompactNumber, formatRelativeTime } from '@/utils/format'
import { deleteAdminOpenClawInstance, listAdminOpenClawAccessRequests, listAdminOpenClawInstances, reconcileAdminOpenClawInstance, reviewAdminOpenClawAccessRequest, setAdminOpenClawAccessPolicy, type OpenClawAccessRequest, type OpenClawAdminConnection, type OpenClawAdminInstance } from '@/api/openclaw'

const items = ref<OpenClawAdminInstance[]>([])
const loading = ref(true)
const error = ref('')
const requests = ref<OpenClawAccessRequest[]>([])
const search = ref('')
const decisionBusy = ref<number | null>(null)
const policyBusy = ref<number | null>(null)
const reconcileBusy = ref<number | null>(null)
type SortKey = 'newest' | 'spend' | 'requests' | 'recent'
const sortKey = ref<SortKey>('newest')
const sortOptions: { value: SortKey; label: string }[] = [
  { value: 'newest', label: 'Newest first' },
  { value: 'spend', label: 'Highest spend' },
  { value: 'requests', label: 'Most requests' },
  { value: 'recent', label: 'Recently active' },
]
// A service counts as connected only when its provider set-up finished. Rows
// still waiting for a QR scan, a Telegram claim or a Google code are set-ups
// that were started and not finished, so they are reported as such instead of
// being folded into a "configured" total. Workspaces queued for deletion no
// longer serve anyone and are left out.
const services = computed(() => {
  let connected = 0
  let unfinished = 0
  for (const item of items.value) {
    if (item.deletion_scheduled_at) continue
    for (const connection of connectionsOf(item)) {
      if (connection.status === 'connected') connected++
      else unfinished++
    }
  }
  return { connected, unfinished }
})
const metrics = computed(() => {
  const totals = items.value.reduce((sum, item) => ({
    running: sum.running + (item.status === 'ready' || item.status === 'running' ? 1 : 0),
    requests: sum.requests + item.request_count,
    requests7d: sum.requests7d + item.requests_7d,
    cost: sum.cost + item.usage_cost,
    cost7d: sum.cost7d + item.usage_cost_7d,
  }), { running: 0, requests: 0, requests7d: 0, cost: 0, cost7d: 0 })
  const { connected, unfinished } = services.value
  const servicesHint = unfinished ? `${plural(unfinished, 'set-up')} not finished` : connected ? 'All set-ups finished' : 'No services set up yet'
  return [
    { label: 'Workspaces', value: count(items.value.length), hint: `${count(totals.running)} running` },
    { label: 'Connected services', value: count(connected), hint: servicesHint },
    { label: 'API requests', value: count(totals.requests), hint: `${count(totals.requests7d)} in the last 7 days` },
    { label: 'Billed', value: money(totals.cost), hint: `${money(totals.cost7d)} in the last 7 days` },
  ]
})
const filteredItems = computed(() => {
  const query = search.value.trim().toLowerCase()
  const matches = query ? items.value.filter(item => [item.user_email, item.name, item.tenant_slug, item.billing_group].some(value => value.toLowerCase().includes(query))) : items.value
  if (sortKey.value === 'newest') return matches
  const lastRequest = (item: OpenClawAdminInstance) => (item.last_request_at ? Date.parse(item.last_request_at) : 0) || 0
  const rank: Record<Exclude<SortKey, 'newest'>, (item: OpenClawAdminInstance) => number> = {
    spend: item => item.usage_cost,
    requests: item => item.request_count,
    recent: lastRequest,
  }
  const score = rank[sortKey.value]
  return [...matches].sort((a, b) => score(b) - score(a))
})

function count(value: number) { return value.toLocaleString() }
function connectionsOf(item: OpenClawAdminInstance): OpenClawAdminConnection[] { return item.connections ?? [] }
function connectionStatus(status: string) { return status === 'connected' ? 'Connected' : status === 'pending' ? 'Set-up not finished' : status }
function modelsSource(item: OpenClawAdminInstance) {
  return item.models_source === 'group' ? `From the ${item.billing_group || 'billing'} group's model allowlist` : 'Built-in defaults: the group allows every model'
}
function modelSyncClass(state: string) {
  return state === 'synced' ? 'text-green-700 dark:text-green-300' : state === 'failed' ? 'text-red-700 dark:text-red-300' : 'text-gray-500'
}
function modelSyncLabel(state: string) {
  return state === 'synced' ? 'Workspace up to date' : state === 'failed' ? 'Workspace update failed; retrying' : 'Updating the workspace…'
}
function plural(value: number, noun: string) { return `${count(value)} ${noun}${value === 1 ? '' : 's'}` }
// Ledger amounts are USD; the console displays CNY like every other billing view.
function money(usd: number) { const value = cnyCost(usd); return value === '-' ? '—' : `¥${value}` }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [nextItems, nextRequests] = await Promise.all([listAdminOpenClawInstances(), listAdminOpenClawAccessRequests()])
    items.value = nextItems
    requests.value = nextRequests
  } catch (err: any) { error.value = err?.message || 'Fleet could not be loaded.' } finally { loading.value = false }
}

async function remove(item: OpenClawAdminInstance) {
  if (!window.confirm(`Queue ${item.name} for deletion? Claw Hub I cleanup runs after seven days.`)) return
  try { const queued = await deleteAdminOpenClawInstance(item.id); items.value = items.value.map(current => current.id === item.id ? { ...current, ...queued } : current) } catch (err: any) { error.value = err?.message || 'Workspace deletion could not be queued.' }
}

async function reconcile(item: OpenClawAdminInstance) {
  reconcileBusy.value = item.id
  try {
    await reconcileAdminOpenClawInstance(item.id)
    await load()
  } catch (err: any) { error.value = err?.message || 'OpenClaw binding could not be repaired.' } finally { reconcileBusy.value = null }
}

function formatDate(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }

async function decide(request: OpenClawAccessRequest, approved: boolean) {
  decisionBusy.value = request.id
  try { await reviewAdminOpenClawAccessRequest(request.id, approved); requests.value = requests.value.filter(item => item.id !== request.id) } catch (err: any) { error.value = err?.message || 'Access request could not be reviewed.' } finally { decisionBusy.value = null }
}

async function setSelfService(userId: number, enabled: boolean) {
  policyBusy.value = userId
  try {
    await setAdminOpenClawAccessPolicy(userId, enabled)
    if (enabled) requests.value = requests.value.filter(item => item.user_id !== userId)
  } catch (err: any) { error.value = err?.message || 'User policy could not be changed.' } finally { policyBusy.value = null }
}

onMounted(load)
</script>

<style scoped>
.fleet-page { width: 100%; min-width: 0; padding: 4px 0 48px; container: fleet / inline-size; }
.fleet-table { width: 100%; }
.fleet-table td { vertical-align: top; }
.fleet-table td[data-label="Models"] { min-width: 240px; max-width: 340px; }
.fleet-models { display: grid; gap: 6px; }
.fleet-usage { display: grid; gap: 2px; }
.fleet-usage-cost { font-weight: 600; font-variant-numeric: tabular-nums; }
.fleet-models li { padding: 4px 6px; border: 1px solid var(--ag-rule); overflow-wrap: anywhere; }
.fleet-table td[data-label="Services"] { min-width: 180px; }
.fleet-services { display: grid; gap: 6px; }
.fleet-services li { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 6px; overflow-wrap: anywhere; }
.fleet-service-state { padding: 1px 6px; border: 1px solid var(--ag-rule); border-radius: 9999px; }
.fleet-table td[data-label="Models"] > div { overflow-wrap: anywhere; }
.fleet-table td[data-label="Action"] > div { flex-wrap: wrap; }
@container fleet (max-width: 1199px) {
  .fleet-table, .fleet-table tbody { display: block; min-width: 0; }
  .fleet-table thead { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip-path: inset(50%); }
  .fleet-table tbody { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 380px), 1fr)); gap: 16px; padding: 16px; }
  .fleet-table tbody tr { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-content: start; border: 1px solid var(--ag-rule); }
  .fleet-table td { display: block; min-width: 0; max-width: none; white-space: normal; overflow-wrap: anywhere; padding: 12px; text-align: left; border: 0; }
  .fleet-table td::before { content: attr(data-label); display: block; margin-bottom: 6px; font-size: 11px; color: var(--ag-ink); opacity: .65; }
  .fleet-table td[data-label="Models"], .fleet-table td[data-label="Action"], .fleet-table td[data-label="Decision"], .fleet-table td[data-label="Reason"] { grid-column: 1 / -1; min-width: 0; max-width: none; }
  .fleet-table td[data-label="Action"] > div, .fleet-table td[data-label="Decision"] > div { justify-content: flex-start; flex-wrap: wrap; }
}
@container fleet (max-width: 479px) {
  .fleet-table tbody { padding: 12px; }
  .fleet-table tbody tr { grid-template-columns: minmax(0, 1fr); }
  .fleet-table td button { white-space: normal; }
}
</style>
