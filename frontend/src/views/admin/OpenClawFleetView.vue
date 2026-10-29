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
        </article>
      </div>

      <div class="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="border-b border-gray-200 px-4 py-4 dark:border-dark-700">
          <h2 class="font-semibold text-gray-900 dark:text-white">Customer workspaces</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">openai-龙虾 ×0.3 pricing, key binding, model policy, usage, and bridge health are visible here.</p>
          <input v-model="search" class="input mt-3 w-full max-w-md" placeholder="Search user or workspace" />
        </div>
        <div v-if="loading" class="p-8 text-center text-sm text-gray-500">Loading fleet…</div>
        <div v-else-if="!filteredItems.length" class="p-8 text-center text-sm text-gray-500">No matching customer workspaces.</div>
        <div v-else class="overflow-x-auto">
          <table class="fleet-table min-w-full divide-y divide-gray-200 text-left text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
              <tr><th class="px-4 py-3">Workspace</th><th class="px-4 py-3">User</th><th class="px-4 py-3">State</th><th class="px-4 py-3">Billing</th><th class="px-4 py-3">Models</th><th class="px-4 py-3">Usage</th><th class="px-4 py-3">Deletion</th><th class="px-4 py-3 text-right">Action</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in filteredItems" :key="item.id" class="text-gray-700 dark:text-gray-200">
                <td data-label="Workspace" class="whitespace-nowrap px-4 py-4"><div class="font-medium">{{ item.name }}</div><div class="text-xs text-gray-400">{{ item.tenant_slug }}</div></td>
                <td data-label="User" class="whitespace-nowrap px-4 py-4">{{ item.user_email }}</td>
                <td data-label="State" class="whitespace-nowrap px-4 py-4"><span class="rounded-full border px-2 py-1 text-xs">{{ item.status }}</span></td>
                <td data-label="Billing" class="whitespace-nowrap px-4 py-4"><div class="font-medium">{{ item.billing_group || 'Unbound' }}</div><div class="text-xs text-gray-500">x{{ item.billing_rate.toFixed(2) }} · {{ item.api_key_status || 'no key' }}</div></td>
                <td data-label="Models" class="max-w-sm px-4 py-4"><ul v-if="item.mapping_enabled" class="fleet-models text-xs"><li v-for="model in item.allowed_models" :key="model">{{ model }}</li><li v-if="!item.allowed_models.length">No allowed models</li></ul><div v-else class="text-xs">Mapping not configured</div><div class="mt-1 text-xs text-gray-500">Default: {{ item.default_model || '—' }}</div></td>
                <td data-label="Usage" class="whitespace-nowrap px-4 py-4"><div>{{ item.usage_count }} calls</div><div class="text-xs text-gray-500">${{ item.usage_cost.toFixed(4) }}</div></td>
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
import { deleteAdminOpenClawInstance, listAdminOpenClawAccessRequests, listAdminOpenClawInstances, reconcileAdminOpenClawInstance, reviewAdminOpenClawAccessRequest, setAdminOpenClawAccessPolicy, type OpenClawAccessRequest, type OpenClawAdminInstance } from '@/api/openclaw'

const items = ref<OpenClawAdminInstance[]>([])
const loading = ref(true)
const error = ref('')
const requests = ref<OpenClawAccessRequest[]>([])
const search = ref('')
const decisionBusy = ref<number | null>(null)
const policyBusy = ref<number | null>(null)
const reconcileBusy = ref<number | null>(null)
const metrics = computed(() => [
  { label: 'Workspaces', value: items.value.length },
  { label: 'Running', value: items.value.filter(item => item.status === 'ready' || item.status === 'running').length },
  { label: 'Connected services', value: items.value.reduce((sum, item) => sum + item.connected_count, 0) },
  { label: 'Recorded usage', value: items.value.reduce((sum, item) => sum + item.usage_count, 0) },
])
const filteredItems = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return items.value
  return items.value.filter(item => [item.user_email, item.name, item.tenant_slug, item.billing_group].some(value => value.toLowerCase().includes(query)))
})

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
.fleet-models li { padding: 4px 6px; border: 1px solid var(--ag-rule); overflow-wrap: anywhere; }
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
